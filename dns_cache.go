package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

const defaultIPv6LookupCacheTTL = 30 * time.Second

var ipv6DNSCache = newIPv6LookupCache(defaultIPv6LookupCacheTTL)

type ipv6LookupCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]ipv6LookupCacheEntry
}

type ipv6LookupCacheEntry struct {
	expiresAt time.Time
	addrs     []net.IPAddr
}

func newIPv6LookupCache(ttl time.Duration) *ipv6LookupCache {
	return &ipv6LookupCache{
		ttl:     ttl,
		entries: make(map[string]ipv6LookupCacheEntry),
	}
}

func configureIPv6LookupCache(ttl time.Duration) {
	ipv6DNSCache = newIPv6LookupCache(ttl)
}

func lookupIPv6Addrs(ctx context.Context, host string) ([]net.IPAddr, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if cachedAddrs, ok := ipv6DNSCache.lookup(host); ok {
		return cachedAddrs, nil
	}

	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", host, err)
	}

	ipv6Addrs := make([]net.IPAddr, 0, len(ipAddrs))
	for _, ipAddr := range ipAddrs {
		if ipAddr.IP == nil || ipAddr.IP.To4() != nil {
			continue
		}
		ipv6Addrs = append(ipv6Addrs, cloneIPAddr(ipAddr))
	}

	if len(ipv6Addrs) == 0 {
		return nil, fmt.Errorf("target %s has no IPv6 address", host)
	}

	ipv6DNSCache.store(host, ipv6Addrs)
	return cloneIPAddrSlice(ipv6Addrs), nil
}

func (cache *ipv6LookupCache) lookup(host string) ([]net.IPAddr, bool) {
	if cache == nil || cache.ttl <= 0 {
		return nil, false
	}

	now := time.Now()

	cache.mu.RLock()
	entry, found := cache.entries[host]
	cache.mu.RUnlock()
	if !found {
		return nil, false
	}
	if !entry.expiresAt.After(now) {
		cache.mu.Lock()
		delete(cache.entries, host)
		cache.mu.Unlock()
		return nil, false
	}

	return cloneIPAddrSlice(entry.addrs), true
}

func (cache *ipv6LookupCache) store(host string, addrs []net.IPAddr) {
	if cache == nil || cache.ttl <= 0 || len(addrs) == 0 {
		return
	}

	cache.mu.Lock()
	cache.entries[host] = ipv6LookupCacheEntry{
		expiresAt: time.Now().Add(cache.ttl),
		addrs:     cloneIPAddrSlice(addrs),
	}
	cache.mu.Unlock()
}

func cloneIPAddrSlice(addrs []net.IPAddr) []net.IPAddr {
	cloned := make([]net.IPAddr, 0, len(addrs))
	for _, addr := range addrs {
		cloned = append(cloned, cloneIPAddr(addr))
	}
	return cloned
}

func cloneIPAddr(addr net.IPAddr) net.IPAddr {
	cloned := net.IPAddr{Zone: addr.Zone}
	if addr.IP != nil {
		cloned.IP = append(net.IP(nil), addr.IP...)
	}
	return cloned
}
