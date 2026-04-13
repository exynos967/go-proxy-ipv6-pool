package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
)

var cidr string
var port int
var username string
var password string
var dialParallel int

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

	setupHTTPProxy(username, password)
	setupSocks5Server(username, password)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		err := listenAndServeSocks5(fmt.Sprintf("0.0.0.0:%d", socks5Port))
		if err != nil {
			log.Fatal("socks5 Server err:", err)
		}

	}()
	go func() {
		err := newHTTPServer(fmt.Sprintf("0.0.0.0:%d", httpPort), httpProxy).ListenAndServe()
		if err != nil {
			log.Fatal("http Server err", err)
		}
	}()

	log.Println("server running ...")
	log.Printf("http running on 0.0.0.0:%d", httpPort)
	log.Printf("socks5 running on 0.0.0.0:%d", socks5Port)
	log.Printf("ipv6 cidr:[%s]", cidr)
	log.Printf("dial parallelism:%d", dialParallel)
	if authEnabled(username, password) {
		log.Printf("proxy auth enabled for username:%s", username)
	}
	wg.Wait()

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
