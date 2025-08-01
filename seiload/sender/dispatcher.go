package sender

import (
	"context"
	"fmt"
	"sync"
	"time"
	"log"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/utils2"
	"github.com/sei-protocol/sei-chain/seiload/generator"
	"github.com/sei-protocol/sei-chain/seiload/stats"
)

// Dispatcher continuously generates transactions and dispatches them to the sender
type Dispatcher struct {
	generator  generator.Generator
	prewarmGen utils.Option[generator.Generator] // Optional prewarm generator
	sender     TxSender

	// Configuration
	limiter  *rate.Limiter

	// Statistics
	totalSent uint64
	mu        sync.RWMutex
	collector *stats.Collector
	logger    *stats.Logger
}

// NewDispatcher creates a new dispatcher
func NewDispatcher(gen generator.Generator, sender TxSender) *Dispatcher {
	return &Dispatcher{
		generator: gen,
		sender:    sender,
		limiter: rate.NewLimiter(rate.Inf, 1), // No rate limiting by default
	}
}

// SetRateLimit sets the minimum time between transaction generations
func (d *Dispatcher) SetRateLimit(duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.limiter = rate.NewLimiter(rate.Every(duration),1)
}

// SetStatsCollector sets the statistics collector for this dispatcher
func (d *Dispatcher) SetStatsCollector(collector *stats.Collector, logger *stats.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.collector = collector
	d.logger = logger
}

// SetPrewarmGenerator sets the prewarm generator for this dispatcher
func (d *Dispatcher) SetPrewarmGenerator(prewarmGen generator.Generator) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prewarmGen = utils.Some(prewarmGen)
}

// Prewarm runs the prewarm generator to completion before starting the main load test
func (d *Dispatcher) Prewarm(ctx context.Context) error {
	d.mu.RLock()
	prewarmGen := d.prewarmGen
	d.mu.RUnlock()

	gen, ok := prewarmGen.Get()
	if !ok { return nil } // No prewarming configured

	log.Print("🔥 Starting account prewarming...")
	processedAccounts := 0
	logInterval := 100

	// Run prewarm generator until completion
	for {
		tx, ok := gen.Generate()
		if !ok {
			break // Prewarming is complete
		}

		// Send the prewarming transaction
		if err := d.sender.Send(ctx, tx); err != nil {
			log.Printf("🔥 Failed to send prewarm transaction for account %s: %v", tx.Scenario.Sender.Address.Hex(), err)
		}

		processedAccounts++

		// Log progress periodically
		if processedAccounts%logInterval == 0 {
			log.Printf("🔥 Prewarming progress: %d accounts processed...", processedAccounts)
		}
	}

	log.Printf("🔥 Prewarming complete! Processed %d accounts", processedAccounts)
	return nil
}

// Start begins the dispatcher's transaction generation and sending loop
func (d *Dispatcher) Run(ctx context.Context) error {
	d.mu.RLock()
	limiter := d.limiter
	d.mu.RUnlock()

	for {
		if err:=limiter.Wait(ctx); err!=nil {
			return err
		}
		// Generate a transaction from main generator
		tx, ok := d.generator.Generate()
		if !ok {
			log.Print("Dispatcher: Generator returned no more transactions")
			return nil
		}

		// Send the transaction
		if err := d.sender.Send(ctx, tx); err != nil {
			return err
		} 
		d.mu.Lock()
		d.totalSent++
		d.mu.Unlock()
	}
}

// StartBatch generates and sends a specific number of transactions then stops
func (d *Dispatcher) RunBatch(ctx context.Context, count int) error {
	if count <= 0 {
		return fmt.Errorf("count must be positive")
	}
	d.mu.RLock()
	limiter := d.limiter
	d.mu.RUnlock()
	for i := range count {
		if err:=limiter.Wait(ctx); err!=nil {
			return err
		}
		// Generate a transaction
		tx, ok := d.generator.Generate()
		if !ok {
			return fmt.Errorf("Dispatcher: Generator returned nil transaction (batch %d/%d)\n", i+1, count)
		}
		// Send the transaction
		if err := d.sender.Send(ctx, tx); err != nil {
			log.Printf("Dispatcher: Failed to send transaction %d/%d: %v", i+1, count, err)
			// Continue despite errors
		} else {
			d.mu.Lock()
			d.totalSent++
			d.mu.Unlock()
		}
	}
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
