package balancer

import (
	"sync"
	"sync/atomic"
)

// RoundRobinBalancer implements round-robin load balancing
type RoundRobinBalancer struct {
	backends []string
	counter  uint32
	mu       sync.RWMutex
}

// NewRoundRobinBalancer creates a new round-robin balancer
func NewRoundRobinBalancer(backends []string) *RoundRobinBalancer {
	return &RoundRobinBalancer{
		backends: backends,
		counter:  0,
	}
}

// Next returns the next backend in round-robin order
func (b *RoundRobinBalancer) Next() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.backends) == 0 {
		return ""
	}

	index := atomic.AddUint32(&b.counter, 1)
	return b.backends[int(index-1)%len(b.backends)]
}

// UpdateBackends updates the list of backends
func (b *RoundRobinBalancer) UpdateBackends(backends []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backends = backends
	atomic.StoreUint32(&b.counter, 0)
}

// GetBackends returns current backends
func (b *RoundRobinBalancer) GetBackends() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]string{}, b.backends...)
}
