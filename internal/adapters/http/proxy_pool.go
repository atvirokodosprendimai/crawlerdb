package fetcher

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
)

// RotationStrategy defines how proxies are selected.
type RotationStrategy string

const (
	RotationRoundRobin RotationStrategy = "round_robin"
	RotationRandom     RotationStrategy = "random"
	RotationLeastUsed  RotationStrategy = "least_used"
)

// ProxyPool manages a pool of proxy servers.
type ProxyPool struct {
	mu       sync.Mutex
	proxies  []*proxyEntry
	strategy RotationStrategy
	counter  atomic.Uint64
}

type proxyEntry struct {
	url   *url.URL
	used  int
	alive bool
}

// NewProxyPool creates a proxy pool from a list of proxy URLs.
func NewProxyPool(proxyURLs []string, strategy RotationStrategy) (*ProxyPool, error) {
	entries := make([]*proxyEntry, 0, len(proxyURLs))
	for _, raw := range proxyURLs {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse proxy URL %q: %w", raw, err)
		}
		entries = append(entries, &proxyEntry{url: u, alive: true})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no proxies provided")
	}

	return &ProxyPool{
		proxies:  entries,
		strategy: strategy,
	}, nil
}

// Next returns the next proxy URL according to the rotation strategy.
func (p *ProxyPool) Next() *url.URL {
	p.mu.Lock()
	defer p.mu.Unlock()

	alive := p.aliveProxies()
	if len(alive) == 0 {
		return nil
	}

	var selected *proxyEntry
	switch p.strategy {
	case RotationRoundRobin:
		idx := p.counter.Add(1) - 1
		selected = alive[idx%uint64(len(alive))]
	case RotationRandom:
		selected = alive[rand.IntN(len(alive))]
	case RotationLeastUsed:
		selected = alive[0]
		for _, e := range alive[1:] {
			if e.used < selected.used {
				selected = e
			}
		}
	default:
		selected = alive[0]
	}

	selected.used++
	return selected.url
}

// MarkDead marks a proxy as unhealthy.
func (p *ProxyPool) MarkDead(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.proxies {
		if e.url.String() == proxyURL {
			e.alive = false
			return
		}
	}
}

// MarkAlive marks a proxy as healthy again.
func (p *ProxyPool) MarkAlive(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.proxies {
		if e.url.String() == proxyURL {
			e.alive = true
			return
		}
	}
}

// AliveCount returns the number of alive proxies.
func (p *ProxyPool) AliveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.aliveProxies())
}

func (p *ProxyPool) aliveProxies() []*proxyEntry {
	var alive []*proxyEntry
	for _, e := range p.proxies {
		if e.alive {
			alive = append(alive, e)
		}
	}
	return alive
}

// Transport returns an http.RoundTripper that uses this proxy pool.
func (p *ProxyPool) Transport() http.RoundTripper {
	return &http.Transport{
		Proxy: func(_ *http.Request) (*url.URL, error) {
			proxy := p.Next()
			if proxy == nil {
				return nil, fmt.Errorf("no alive proxies")
			}
			return proxy, nil
		},
	}
}
