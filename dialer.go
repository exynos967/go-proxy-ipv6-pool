package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

const (
	ipv6DialTimeout = 10 * time.Second
)

func dialTCPViaRandomIPv6(ctx context.Context, target, tag string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resolveCtx, cancel := context.WithTimeout(ctx, ipv6DialTimeout)
	defer cancel()

	remoteAddrs, err := resolveIPv6TCPAddrs(resolveCtx, target)
	if err != nil {
		return nil, err
	}

	type dialResult struct {
		conn       net.Conn
		err        error
		outgoingIP string
		remoteAddr string
		candidate  int
	}

	raceCtx, cancelRace := context.WithCancel(ctx)
	defer cancelRace()

	results := make(chan dialResult, dialParallel)
	for candidate := 1; candidate <= dialParallel; candidate++ {
		go func(candidate int) {
			conn, outgoingIP, remoteAddr, err := dialTCPViaSingleRandomIPv6(raceCtx, target, tag, candidate, remoteAddrs)
			results <- dialResult{
				conn:       conn,
				err:        err,
				outgoingIP: outgoingIP,
				remoteAddr: remoteAddr,
				candidate:  candidate,
			}
		}(candidate)
	}

	var winner net.Conn
	var dialErrors []error
	for i := 0; i < dialParallel; i++ {
		result := <-results
		if result.err != nil {
			dialErrors = append(dialErrors, result.err)
			continue
		}
		if winner == nil {
			winner = result.conn
			cancelRace()
			continue
		}
		_ = result.conn.Close()
	}

	if winner != nil {
		return winner, nil
	}

	return nil, summarizeDialErrors(
		fmt.Sprintf("parallel dial %s failed after %d candidates", target, dialParallel),
		dialErrors,
	)
}

func newRandomLocalTCPAddr(ipv6CIDR string) (*net.TCPAddr, string, error) {
	outgoingIP, err := generateRandomIPv6(ipv6CIDR)
	if err != nil {
		return nil, "", fmt.Errorf("generate random IPv6: %w", err)
	}

	localAddr, err := net.ResolveTCPAddr("tcp6", net.JoinHostPort(outgoingIP, "0"))
	if err != nil {
		return nil, "", fmt.Errorf("resolve local IPv6 %s: %w", outgoingIP, err)
	}

	return localAddr, outgoingIP, nil
}

func dialTCPViaSingleRandomIPv6(
	ctx context.Context,
	target string,
	tag string,
	candidate int,
	remoteAddrs []*net.TCPAddr,
) (net.Conn, string, string, error) {
	localAddr, outgoingIP, err := newRandomLocalTCPAddr(cidr)
	if err != nil {
		return nil, "", "", err
	}

	dialer := &net.Dialer{
		LocalAddr:     localAddr,
		Timeout:       ipv6DialTimeout,
		FallbackDelay: -1,
	}

	var dialErrors []error
	for _, remoteAddr := range remoteAddrs {
		conn, err := dialer.DialContext(ctx, "tcp6", remoteAddr.String())
		if err == nil {
			return conn, outgoingIP, remoteAddr.String(), nil
		}
		if ctx.Err() != nil {
			return nil, outgoingIP, remoteAddr.String(), ctx.Err()
		}

		dialErrors = append(dialErrors, fmt.Errorf("candidate %d via %s -> %s: %w", candidate, outgoingIP, remoteAddr.String(), err))
	}

	return nil, outgoingIP, "", summarizeDialErrors(
		fmt.Sprintf("[%s] dial %s via %s failed", tag, target, outgoingIP),
		dialErrors,
	)
}

func resolveIPv6TCPAddrs(ctx context.Context, target string) ([]*net.TCPAddr, error) {
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("split host/port %s: %w", target, err)
	}

	port, err := parseTCPPort(portText)
	if err != nil {
		return nil, err
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return nil, fmt.Errorf("target %s is IPv4 only", target)
		}
		return []*net.TCPAddr{{IP: ip, Port: port}}, nil
	}

	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", host, err)
	}

	var tcpAddrs []*net.TCPAddr
	for _, ipAddr := range ipAddrs {
		if ipAddr.IP == nil || ipAddr.IP.To4() != nil {
			continue
		}
		tcpAddrs = append(tcpAddrs, &net.TCPAddr{IP: ipAddr.IP, Port: port, Zone: ipAddr.Zone})
	}

	if len(tcpAddrs) == 0 {
		return nil, fmt.Errorf("target %s has no IPv6 address", target)
	}

	return tcpAddrs, nil
}

func parseTCPPort(portText string) (int, error) {
	port, err := strconv.Atoi(portText)
	if err == nil {
		return port, nil
	}

	port, err = net.LookupPort("tcp", portText)
	if err != nil {
		return 0, fmt.Errorf("resolve tcp port %s: %w", portText, err)
	}
	return port, nil
}

func summarizeDialErrors(prefix string, errs []error) error {
	if len(errs) == 0 {
		return fmt.Errorf("%s", prefix)
	}
	if len(errs) == 1 {
		return fmt.Errorf("%s: %w", prefix, errs[0])
	}
	return fmt.Errorf("%s: %w (and %d more errors)", prefix, errs[0], len(errs)-1)
}
