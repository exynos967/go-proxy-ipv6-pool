package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
)

const proxyAuthRealm = "proxy-ipv6-pool"

var httpProxy *goproxy.ProxyHttpServer

func setupHTTPProxy(authUser, authPassword string) {
	httpProxy = goproxy.NewProxyHttpServer()
	httpProxy.Verbose = false
	httpProxy.Tr.DisableKeepAlives = true
	httpProxy.Tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialTCPViaRandomIPv6(ctx, addr, "http")
		if err != nil {
			log.Printf("[http] Dial to %s error: %v", addr, err)
			return nil, err
		}
		return conn, nil
	}
	httpProxy.ConnectDialWithReq = func(req *http.Request, network, addr string) (net.Conn, error) {
		conn, err := dialTCPViaRandomIPv6(req.Context(), addr, "http-connect")
		if err != nil {
			log.Printf("[http] CONNECT to %s error: %v", addr, err)
			return nil, err
		}
		return conn, nil
	}
	configureHTTPProxyConnect(httpProxy, authUser, authPassword)
	configureHTTPProxyAuth(httpProxy, authUser, authPassword)
}

func configureHTTPProxyAuth(proxy *goproxy.ProxyHttpServer, authUser, authPassword string) {
	if !authEnabled(authUser, authPassword) {
		return
	}

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if hasValidHTTPProxyCredentials(req, authUser, authPassword) {
			return req, nil
		}
		return req, newHTTPProxyAuthRequiredResponse(req)
	})
}

func configureHTTPProxyConnect(proxy *goproxy.ProxyHttpServer, authUser, authPassword string) {
	proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if authEnabled(authUser, authPassword) && !hasValidHTTPProxyCredentials(ctx.Req, authUser, authPassword) {
			ctx.Resp = newHTTPProxyAuthRequiredResponse(ctx.Req)
			return goproxy.RejectConnect, host
		}
		target, err := validateConnectHost(host)
		if err != nil {
			ctx.Resp = newHTTPBadRequestResponse(ctx.Req, err.Error()+"\n")
			return goproxy.RejectConnect, host
		}
		return newHTTPConnectAction(target), host
	})
}

func newHTTPConnectAction(target string) *goproxy.ConnectAction {
	return &goproxy.ConnectAction{
		Action: goproxy.ConnectHijack,
		Hijack: func(req *http.Request, client net.Conn, ctx *goproxy.ProxyCtx) {
			clearConnDeadline(client)

			targetConn, err := dialTCPViaRandomIPv6(req.Context(), target, "http-connect")
			if err != nil {
				log.Printf("[http] CONNECT to %s error: %v", target, err)
				writeHTTPConnectError(client, err)
				return
			}

			if _, err := io.WriteString(client, "HTTP/1.0 200 Connection established\r\n\r\n"); err != nil {
				_ = targetConn.Close()
				_ = client.Close()
				return
			}

			proxyHTTPConnectTunnel(client, targetConn)
		},
	}
}

func validateConnectHost(host string) (string, error) {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host, nil
	}
	return "", fmt.Errorf("CONNECT target must include port")
}

func clearConnDeadline(conn net.Conn) {
	_ = conn.SetDeadline(time.Time{})
}

func writeHTTPConnectError(client net.Conn, err error) {
	body := err.Error()
	response := fmt.Sprintf(
		"HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
		len(body),
		body,
	)
	_, _ = io.WriteString(client, response)
	_ = client.Close()
}

type tunnelHalfClosable interface {
	net.Conn
	CloseWrite() error
	CloseRead() error
}

func proxyHTTPConnectTunnel(client, target net.Conn) {
	targetTCP, targetOK := target.(tunnelHalfClosable)
	clientTCP, clientOK := client.(tunnelHalfClosable)
	if targetOK && clientOK {
		go tunnelCopyAndClose(targetTCP, clientTCP)
		go tunnelCopyAndClose(clientTCP, targetTCP)
		return
	}

	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go tunnelCopyOrClose(target, client, &wg)
		go tunnelCopyOrClose(client, target, &wg)
		wg.Wait()
		_ = client.Close()
		_ = target.Close()
	}()
}

func tunnelCopyAndClose(dst, src tunnelHalfClosable) {
	_, _ = io.Copy(dst, src)
	_ = dst.CloseWrite()
	_ = src.CloseRead()
}

func tunnelCopyOrClose(dst io.Writer, src io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
}

func hasValidHTTPProxyCredentials(req *http.Request, expectedUser, expectedPassword string) bool {
	if req == nil {
		return false
	}

	authHeader := strings.TrimSpace(req.Header.Get("Proxy-Authorization"))
	if authHeader == "" {
		return false
	}

	scheme, encoded, found := strings.Cut(authHeader, " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return false
	}

	rawCredentials, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return false
	}

	user, pass, found := strings.Cut(string(rawCredentials), ":")
	if !found {
		return false
	}

	return user == expectedUser && pass == expectedPassword
}

func newHTTPProxyAuthRequiredResponse(req *http.Request) *http.Response {
	response := goproxy.NewResponse(
		req,
		goproxy.ContentTypeText,
		http.StatusProxyAuthRequired,
		"proxy authentication required\n",
	)
	response.Header.Set("Proxy-Authenticate", `Basic realm="`+proxyAuthRealm+`"`)
	return response
}

func newHTTPBadRequestResponse(req *http.Request, body string) *http.Response {
	return goproxy.NewResponse(
		req,
		goproxy.ContentTypeText,
		http.StatusBadRequest,
		body,
	)
}
