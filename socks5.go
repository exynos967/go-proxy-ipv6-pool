package main

import (
	"context"
	"fmt"
	"log"
	"net"

	socks5 "github.com/armon/go-socks5"
)

var socks5Conf = &socks5.Config{}
var socks5Server *socks5.Server

func setupSocks5Server(authUser, authPassword string) {
	socks5Conf = &socks5.Config{
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialTCPViaRandomIPv6(ctx, addr, "socks5")
			if err != nil {
				log.Printf("[socks5] Dial to %s error: %v", addr, err)
				return nil, err
			}
			return conn, nil
		},
		Resolver: ipv6OnlySocksResolver{},
		Rewriter: preserveSocks5FQDNRewriter{},
	}
	if authEnabled(authUser, authPassword) {
		socks5Conf.Credentials = socks5.StaticCredentials{
			authUser: authPassword,
		}
	}
	var err error
	socks5Server, err = socks5.New(socks5Conf)
	if err != nil {
		log.Fatal(err)
	}
}

type ipv6OnlySocksResolver struct{}

func (ipv6OnlySocksResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ipAddrs, err := lookupIPv6Addrs(ctx, name)
	if err != nil {
		return ctx, nil, err
	}

	randomIndex, randomErr := randomInt(len(ipAddrs))
	if randomErr != nil {
		randomIndex = 0
	}
	if ipAddrs[randomIndex].IP == nil {
		return ctx, nil, fmt.Errorf("target %s has no IPv6 address", name)
	}
	return ctx, append(net.IP(nil), ipAddrs[randomIndex].IP...), nil
}

type preserveSocks5FQDNRewriter struct{}

func (preserveSocks5FQDNRewriter) Rewrite(ctx context.Context, request *socks5.Request) (context.Context, *socks5.AddrSpec) {
	if request == nil {
		return ctx, nil
	}
	if request.DestAddr == nil || request.DestAddr.FQDN == "" {
		return ctx, request.DestAddr
	}

	return ctx, &socks5.AddrSpec{
		FQDN: request.DestAddr.FQDN,
		Port: request.DestAddr.Port,
	}
}
