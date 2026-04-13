package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var cidr string
var port int
var username string
var password string
var dialParallel int
var dialTimeoutMs int
var dnsCacheTTLSeconds int

func main() {
	defaults, err := loadConfigDefaults()
	if err != nil {
		log.Fatal(err)
	}

	flag.IntVar(&port, "port", defaults.port, "server port")
	flag.StringVar(&cidr, "cidr", defaults.cidr, "ipv6 cidr")
	flag.StringVar(&username, "username", defaults.username, "proxy auth username")
	flag.StringVar(&password, "password", defaults.password, "proxy auth password")
	flag.IntVar(&dialParallel, "dial-parallel", defaults.dialParallel, "parallel IPv6 dial candidates")
	flag.IntVar(&dialTimeoutMs, "dial-timeout-ms", defaults.dialTimeout, "single IPv6 dial timeout in milliseconds")
	flag.IntVar(&dnsCacheTTLSeconds, "dns-cache-ttl-seconds", defaults.dnsCacheTTL, "IPv6 DNS cache TTL in seconds; 0 disables cache")
	flag.Parse()

	if cidr == "" {
		log.Fatal("cidr is empty")
	}
	if err := validateAuthConfig(username, password); err != nil {
		log.Fatal(err)
	}

	httpPort := port
	socks5Port := port + 1

	if socks5Port > 65535 {
		log.Fatal("port too large")
	}
	if dialParallel <= 0 {
		log.Fatal("dial-parallel must be greater than 0")
	}
	if dialTimeoutMs <= 0 {
		log.Fatal("dial-timeout-ms must be greater than 0")
	}
	if dnsCacheTTLSeconds < 0 {
		log.Fatal("dns-cache-ttl-seconds must be greater than or equal to 0")
	}

	ipv6DialTimeout = time.Duration(dialTimeoutMs) * time.Millisecond
	configureIPv6LookupCache(time.Duration(dnsCacheTTLSeconds) * time.Second)

	setupHTTPProxy(username, password)
	setupSocks5Server(username, password)

	log.Println("server running ...")
	log.Printf("http running on 0.0.0.0:%d", httpPort)
	log.Printf("socks5 running on 0.0.0.0:%d", socks5Port)
	log.Printf("ipv6 cidr:[%s]", cidr)
	log.Printf("dial parallelism:%d", dialParallel)
	log.Printf("dial timeout:%s", ipv6DialTimeout)
	log.Printf("dns cache ttl:%s", time.Duration(dnsCacheTTLSeconds)*time.Second)
	if authEnabled(username, password) {
		log.Printf("proxy auth enabled for username:%s", username)
	}
	if err := runProxyServers(httpPort, socks5Port); err != nil {
		log.Fatal(err)
	}

}

func runProxyServers(httpPort, socks5Port int) error {
	httpAddr := fmt.Sprintf("0.0.0.0:%d", httpPort)
	socks5Addr := fmt.Sprintf("0.0.0.0:%d", socks5Port)

	httpListener, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("listen http: %w", err)
	}
	socks5Listener, err := net.Listen("tcp", socks5Addr)
	if err != nil {
		_ = httpListener.Close()
		return fmt.Errorf("listen socks5: %w", err)
	}

	httpServer := newHTTPServer(httpAddr, httpProxy)
	errCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		errCh <- serveHTTPServer(httpServer, httpListener)
	}()
	go func() {
		defer wg.Done()
		errCh <- serveSocks5Listener(socks5Listener)
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var runErr error
	select {
	case <-signalCtx.Done():
		log.Printf("shutdown signal received")
	case runErr = <-errCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), inboundTimeout)
	defer cancel()

	if err := shutdownProxyServers(shutdownCtx, httpServer, socks5Listener); err != nil && runErr == nil {
		runErr = err
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err == nil || isExpectedServerStop(err) {
			continue
		}
		if runErr == nil {
			runErr = err
		}
	}

	if runErr != nil && !isExpectedServerStop(runErr) {
		return runErr
	}

	return nil
}

func generateRandomIPv6(cidr string) (string, error) {
	// 解析CIDR
	_, ipv6Net, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}

	// 获取网络部分和掩码长度
	maskSize, _ := ipv6Net.Mask.Size()

	// 计算随机部分的长度
	randomPartLength := 128 - maskSize

	// 生成随机部分
	randomPart := make([]byte, randomPartLength/8)
	_, err = rand.Read(randomPart)
	if err != nil {
		return "", err
	}

	// 获取网络部分
	networkPart := ipv6Net.IP.To16()

	// 合并网络部分和随机部分
	for i := 0; i < len(randomPart); i++ {
		networkPart[16-len(randomPart)+i] = randomPart[i]
	}

	return networkPart.String(), nil
}

func shutdownProxyServers(ctx context.Context, httpServer *http.Server, socks5Listener net.Listener) error {
	var shutdownErr error
	if err := httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		shutdownErr = fmt.Errorf("shutdown http: %w", err)
	}
	if err := socks5Listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) && shutdownErr == nil {
		shutdownErr = fmt.Errorf("shutdown socks5: %w", err)
	}
	return shutdownErr
}

func isExpectedServerStop(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed)
}
