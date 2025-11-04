package pipeline

import (
	"context"
	"sync"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// OrderingComponent is a long-running component that orders preprocessed blocks sequentially
type OrderingComponent struct {
	mu         sync.RWMutex
	in         <-chan *pipelinetypes.PreprocessedBlock
	out        chan<- *pipelinetypes.PreprocessedBlock
	bufferSize int
	started    bool
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	// Ordering buffer state
	buffer       map[int64]*pipelinetypes.PreprocessedBlock // Maps height to preprocessed block
	nextSequence int64                            // Next expected sequence number
	muBuffer     sync.Mutex                       // Protects buffer and nextSequence
}

// NewOrderingComponent creates a new OrderingComponent
func NewOrderingComponent(
	in <-chan *pipelinetypes.PreprocessedBlock,
	out chan<- *pipelinetypes.PreprocessedBlock,
	bufferSize int,
	startSequence int64,
) *OrderingComponent {
	if bufferSize < 1 {
		bufferSize = 100
	}
	if startSequence < 1 {
		startSequence = 1
	}
	return &OrderingComponent{
		in:           in,
		out:          out,
		bufferSize:   bufferSize,
		nextSequence: startSequence,
		buffer:       make(map[int64]*pipelinetypes.PreprocessedBlock),
	}
}

// Start begins ordering blocks
func (o *OrderingComponent) Start(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.started {
		return
	}

	o.ctx, o.cancel = context.WithCancel(ctx)
	o.started = true
	o.wg.Add(1)

	go func() {
		defer o.wg.Done()
		for {
			select {
			case <-o.ctx.Done():
				return
			case block, ok := <-o.in:
				if !ok {
					return
				}
				o.processBlock(block)
			}
		}
	}()
}

// processBlock processes a block and outputs it if it's the next in sequence
func (o *OrderingComponent) processBlock(block *pipelinetypes.PreprocessedBlock) {
	o.muBuffer.Lock()
	defer o.muBuffer.Unlock()

	height := block.Height

	// Store block in buffer
	o.buffer[height] = block

	// Output blocks in sequence order
	for {
		next, exists := o.buffer[o.nextSequence]
		if !exists {
			break
		}

		// Output the next block
		select {
		case <-o.ctx.Done():
			return
		case o.out <- next:
			delete(o.buffer, o.nextSequence)
			o.nextSequence++
		}
	}
}

// Stop gracefully stops the component
func (o *OrderingComponent) Stop() {
	o.mu.Lock()
	if !o.started {
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()

	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	o.mu.Lock()
	o.started = false
	o.mu.Unlock()
}

// IsRunning returns true if the component is currently running
func (o *OrderingComponent) IsRunning() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.started
}
