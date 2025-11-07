package dag

import (
	"context"
	"fmt"
	"log"
)

// TxHandler represents the actual execution logic for a transaction. It receives the
// TxContext once all dependencies have been satisfied.
type TxHandler func(context.Context, *TxContext) error

// TxContext encapsulates a transaction along with its ordered access operations.
type TxContext struct {
	Index   int
	Name    string
	Handler TxHandler

	AccessOps []*AccessOp
}

// NewTxContext constructs a new transaction context with the provided access operations.
func NewTxContext(index int, name string, handler TxHandler, ops ...*AccessOp) *TxContext {
	tx := &TxContext{
		Index:   index,
		Name:    name,
		Handler: handler,
	}
	tx.AddAccessOps(ops...)
	return tx
}

// AddAccessOps appends additional access operations to the transaction in order.
func (tx *TxContext) AddAccessOps(ops ...*AccessOp) {
	for _, op := range ops {
		if op == nil {
			continue
		}
		op.setParent(tx)
		tx.AccessOps = append(tx.AccessOps, op)
	}
}

// Run waits for all dependencies, executes the transaction handler, and signals completion.
func (tx *TxContext) Run(ctx context.Context) error {
	for _, op := range tx.AccessOps {
		if err := op.Wait(ctx); err != nil {
			return fmt.Errorf("tx %s waiting on %s: %w", tx.Name, op.Name(), err)
		}
	}

	if tx.Handler != nil {
		if err := tx.Handler(ctx, tx); err != nil {
			return fmt.Errorf("tx %s handler error: %w", tx.Name, err)
		}
	} else {
		log.Printf("tx %s executed with no-op handler", tx.Name)
	}

	for _, op := range tx.AccessOps {
		op.Signal()
	}
	return nil
}

// Dependencies returns the dependencies for each access operation, primarily for debugging
// and testing purposes.
func (tx *TxContext) Dependencies() map[string]int {
	deps := make(map[string]int)
	for _, op := range tx.AccessOps {
		deps[op.Name()] = len(op.waitFor)
	}
	return deps
}
