package process

import (
	"fmt"
	"net"
	"sync"
)

// PortAllocator provides dynamic port allocation and conflict resolution.
type PortAllocator struct {
	mu        sync.Mutex
	allocated map[int]bool
	minPort   int
	maxPort   int
	nextPort  int
}

// NewPortAllocator creates a new PortAllocator with default ranges.
func NewPortAllocator() *PortAllocator {
	return &PortAllocator{
		allocated: make(map[int]bool),
		minPort:   10000,
		maxPort:   60000,
		nextPort:  10000,
	}
}

// Allocate finds an available port, marks it as allocated, and returns it.
func (p *PortAllocator) Allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Try OS allocation first
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err == nil {
		l, err := net.ListenTCP("tcp", addr)
		if err == nil {
			port := l.Addr().(*net.TCPAddr).Port
			_ = l.Close()
			if !p.allocated[port] {
				p.allocated[port] = true
				return port, nil
			}
		}
	}

	// 2. Fallback to scanning.
	start := p.nextPort
	for i := 0; i <= (p.maxPort - p.minPort); i++ {
		port := start + i
		if port > p.maxPort {
			port = p.minPort + (port - p.maxPort - 1)
		}

		if !p.allocated[port] {
			// Verify it's actually free by listening
			addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				l, err := net.ListenTCP("tcp", addr)
				if err == nil {
					_ = l.Close()
					p.allocated[port] = true
					p.nextPort = port + 1
					if p.nextPort > p.maxPort {
						p.nextPort = p.minPort
					}
					return port, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("no available ports")
}

// Release marks a port as available.
func (p *PortAllocator) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.allocated, port)
}
