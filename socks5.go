package main

import (
	"context"
	"log"
	"net"

	socks5 "github.com/armon/go-socks5"
)

var socks5Conf = &socks5.Config{}
var socks5Server *socks5.Server

func setupSocks5Server(authUser, authPassword string) {
	// 指定出口 IP 地址
	// 指定本地出口 IPv6 地址

	// 创建一个 SOCKS5 服务器配置
	socks5Conf = &socks5.Config{
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialTCPViaRandomIPv6(ctx, addr, "socks5")
			if err != nil {
				log.Printf("[socks5] Dial to %s error: %v", addr, err)
				return nil, err
			}
			return conn, nil
		},
	}
	if authEnabled(authUser, authPassword) {
		socks5Conf.Credentials = socks5.StaticCredentials{
			authUser: authPassword,
		}
	}
	var err error
	// 创建 SOCKS5 服务器
	socks5Server, err = socks5.New(socks5Conf)
	if err != nil {
		log.Fatal(err)
	}
}
