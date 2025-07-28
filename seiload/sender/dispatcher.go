package sender

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/seiload/generator"
	"github.com/sei-protocol/sei-chain/seiload/stats"
)

// Dispatcher continuously generates transactions and dispatches them to the sender
type Dispatcher struct {
	generator generator.Generator
	sender    TxSender
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// Configuration
	rateLimit time.Duration // Minimum time between transactions

	// Statistics
	totalSent uint64
	mu        sync.RWMutex
	collector *stats.Collector
	logger    *stats.Logger
}

// NewDispatcher creates a new dispatcher
func NewDispatcher(gen generator.Generator, sender TxSender) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	return &Dispatcher{
		generator: gen,
		sender:    sender,
		ctx:       ctx,
		cancel:    cancel,
		rateLimit: 0, // No rate limiting by default
	}
}

// SetRateLimit sets the minimum time between transaction generations
func (d *Dispatcher) SetRateLimit(duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rateLimit = duration
}

// SetStatsCollector sets the statistics collector for the dispatcher
func (d *Dispatcher) SetStatsCollector(collector *stats.Collector, logger *stats.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.collector = collector
	d.logger = logger
}

// Start begins the dispatcher's transaction generation and sending loop
func (d *Dispatcher) Start() {
	d.wg.Add(1)
	go d.dispatchLoop()
}

// Stop gracefully shuts down the dispatcher
func (d *Dispatcher) Stop() {
	d.cancel()
	d.wg.Wait()
}

// dispatchLoop is the main loop that generates and dispatches transactions
func (d *Dispatcher) dispatchLoop() {
	defer d.wg.Done()

	var lastSent time.Time

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			// Check rate limiting
			d.mu.RLock()
			rateLimit := d.rateLimit
			d.mu.RUnlock()

			if rateLimit > 0 {
				elapsed := time.Since(lastSent)
				if elapsed < rateLimit {
					time.Sleep(rateLimit - elapsed)
				}
			}

			// Generate a transaction
			tx := d.generator.Generate()
			if tx == nil {
				fmt.Println("Dispatcher: Generator returned nil transaction")
				continue
			}

			// Send the transaction
			err := d.sender.Send(tx)
			if err != nil {
				fmt.Printf("Dispatcher: Failed to send transaction: %v\n", err)
				// Continue despite errors - could add retry logic here
			} else {
				d.mu.Lock()
				d.totalSent++
				d.mu.Unlock()
			}

			lastSent = time.Now()
		}
	}
}

// StartBatch generates and sends a specific number of transactions then stops
func (d *Dispatcher) StartBatch(count int) error {
	if count <= 0 {
		return fmt.Errorf("count must be positive")
	}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()

		var lastSent time.Time

		for i := 0; i < count; i++ {
			select {
			case <-d.ctx.Done():
				return
			default:
				// Check rate limiting
				d.mu.RLock()
				rateLimit := d.rateLimit
				d.mu.RUnlock()

				if rateLimit > 0 && i > 0 {
					elapsed := time.Since(lastSent)
					if elapsed < rateLimit {
						time.Sleep(rateLimit - elapsed)
					}
				}

				// Generate a transaction
				tx := d.generator.Generate()
				if tx == nil {
					fmt.Printf("Dispatcher: Generator returned nil transaction (batch %d/%d)\n", i+1, count)
					continue
				}

				// Send the transaction
				err := d.sender.Send(tx)
				if err != nil {
					fmt.Printf("Dispatcher: Failed to send transaction %d/%d: %v\n", i+1, count, err)
					// Continue despite errors
				} else {
					d.mu.Lock()
					d.totalSent++
					d.mu.Unlock()
				}

				lastSent = time.Now()
			}
		}
	}()

	return nil
}

// GetStats returns dispatcher statistics
func (d *Dispatcher) GetStats() DispatcherStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return DispatcherStats{
		TotalSent: d.totalSent,
	}
}

// DispatcherStats contains statistics for the dispatcher
type DispatcherStats struct {
	TotalSent uint64
}

// Wait waits for the dispatcher to finish (useful for batch mode)
func (d *Dispatcher) Wait() {
	d.wg.Wait()
}
