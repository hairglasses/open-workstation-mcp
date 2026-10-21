package process

import (
	"net"
	"testing"
)

func TestPortAllocator_AllocateAndRelease(t *testing.T) {
	allocator := NewPortAllocator()

	port1, err := allocator.Allocate()
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}

	if port1 <= 0 {
		t.Errorf("expected valid port, got %d", port1)
	}

	// Verify the port is marked as allocated
	allocator.mu.Lock()
	if !allocator.allocated[port1] {
		t.Errorf("expected port %d to be marked as allocated", port1)
	}
	allocator.mu.Unlock()

	// Ensure port can actually be listened on, wait, we closed it in Allocate
	// let's try to allocate another port
	port2, err := allocator.Allocate()
	if err != nil {
		t.Fatalf("failed to allocate second port: %v", err)
	}

	if port1 == port2 {
		t.Errorf("expected different ports, got %d and %d", port1, port2)
	}

	// Release first port
	allocator.Release(port1)

	allocator.mu.Lock()
	if allocator.allocated[port1] {
		t.Errorf("expected port %d to be released", port1)
	}
	allocator.mu.Unlock()
}

func TestPortAllocator_ConflictResolution(t *testing.T) {
	allocator := NewPortAllocator()

	// Simulate a conflict by pre-allocating a port by listening on it
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve tcp addr: %v", err)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen on tcp addr: %v", err)
	}
	defer func() { _ = l.Close() }()

	conflictedPort := l.Addr().(*net.TCPAddr).Port

	// Force the allocator's OS allocation to fail or fallback by mocking if possible,
	// but here we just ensure that Allocate doesn't return `conflictedPort`.
	// Let's allocate a few ports and ensure none of them are `conflictedPort`.
	for i := 0; i < 5; i++ {
		port, err := allocator.Allocate()
		if err != nil {
			t.Fatalf("failed to allocate port: %v", err)
		}
		if port == conflictedPort {
			t.Errorf("allocator returned a conflicted port: %d", port)
		}
		defer allocator.Release(port)
	}
}

func TestPortAllocator_FallbackScanning(t *testing.T) {
	allocator := NewPortAllocator()
	allocator.minPort = 20000
	allocator.maxPort = 20002
	allocator.nextPort = 20000

	// We can't easily disable OS allocation in this test without mocking it,
	// but we can test that we can exhaust the ports.
	ports := make([]int, 0)
	for i := 0; i < 10; i++ {
		p, err := allocator.Allocate()
		if err == nil {
			ports = append(ports, p)
		}
	}

	if len(ports) == 0 {
		t.Errorf("expected to allocate some ports")
	}

	for _, p := range ports {
		allocator.Release(p)
	}
}
