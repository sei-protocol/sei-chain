package evmrpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

const poolClientTTL = 60 * time.Second

type ClientPool struct {
	mu      sync.RWMutex
	clients map[string]*poolClient
	ttl     time.Duration
	stopOnce sync.Once
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func (p *ClientPool) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		p.wg.Wait()
	})
}

type poolClient struct {
	mu     sync.RWMutex
	client *rpc.Client
}

func newClientPool(ttl time.Duration) *ClientPool {
	return &ClientPool{
		clients: map[string]*poolClient{},
		ttl:     ttl,
		stopCh:  make(chan struct{}),
	}
}

func NewClientPool() *ClientPool {
	return newClientPool(poolClientTTL)
}

func (p *ClientPool) lockClient(ctx context.Context, rawURL string) (*poolClient, error) {
	// Try to lock existing client.
	p.mu.RLock()
	if client, ok := p.clients[rawURL]; ok {
		// Lease the client.
		client.mu.RLock()
		p.mu.RUnlock()
		return client, nil
	}
	p.mu.RUnlock()

	// Establish a new connection.
	conn, err := rpc.DialContext(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("rpc.DialContext(%q): %w", rawURL, err)
	}
	client := &poolClient{client: conn}

	p.mu.Lock()
	defer p.mu.Unlock()
	// Register the new client.
	var ttl time.Duration
	if _, ok := p.clients[rawURL]; !ok {
		p.clients[rawURL] = client
		ttl = p.ttl
	}

	// Lease the client.
	client.mu.RLock()
	// Spawn cleanup task.
	p.wg.Go(func() {
		// Wait until expiry or until pool is closed.
		select {
		case <-time.After(ttl):
		case <-p.stopCh:
		}
		fmt.Printf("expired\n")
		// Remote the client from the list to prevent new leases.
		p.mu.Lock()
		if p.clients[rawURL] == client {
			delete(p.clients, rawURL)
		}
		// Wait for the leases to be released.
		p.mu.Unlock()
		client.mu.Lock()
		defer client.mu.Unlock()
		// Close the client.
		client.client.Close()
		fmt.Printf("closed\n")
	})
	return client, nil
}

func (p *ClientPool) Call(ctx context.Context, rawURL string, result any, method string, args ...any) error {
	client, err := p.lockClient(ctx, rawURL)
	if err != nil {
		return err
	}
	defer client.mu.RUnlock()
	if err := client.client.CallContext(ctx, result, method, args...); err != nil {
		return fmt.Errorf("%s(%q): %w", method, rawURL, err)
	}
	return nil
}
