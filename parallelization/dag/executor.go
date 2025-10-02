package dag

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// DAGExecutor orchestrates the parallel execution of transactions by respecting their access
// dependencies. A goroutine is spawned per transaction.
type DAGExecutor struct {
	txs []*TxContext
}

// NewDAGExecutor constructs a new executor and wires up the dependencies between the provided
// transactions based on their access operations.
func NewDAGExecutor(txs []*TxContext) *DAGExecutor {
	WireDependencies(txs)
	return &DAGExecutor{txs: txs}
}

// Execute runs all transactions and waits for them to complete. If one transaction fails, the
// execution context is cancelled and the first error is returned.
func (e *DAGExecutor) Execute(ctx context.Context) error {
	if len(e.txs) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg     sync.WaitGroup
		errMu  sync.Mutex
		result error
	)

	wg.Add(len(e.txs))
	for _, tx := range e.txs {
		tx := tx
		go func() {
			defer wg.Done()
			if err := tx.Run(ctx); err != nil {
				errMu.Lock()
				if result == nil {
					result = err
					cancel()
				} else {
					result = errors.Join(result, err)
				}
				errMu.Unlock()
			}
		}()
	}

	wg.Wait()
	return result
}

// WireDependencies wires all cross-transaction dependencies according to the RFC rules. It resets
// previously registered dependencies before recomputing them.
func WireDependencies(txs []*TxContext) {
	type resourceState struct {
		lastRead  *AccessOp
		lastWrite *AccessOp
	}

	states := make(map[ResourceID]resourceState)

	for _, tx := range txs {
		for _, op := range tx.AccessOps {
			op.resetDependencies()
		}
	}

	for _, tx := range txs {
		for _, op := range tx.AccessOps {
			state := states[op.Resource()]

			if state.lastWrite != nil && state.lastWrite.parentTx() != tx {
				op.AddDependency(state.lastWrite)
			}

			if op.Access() == AccessTypeWrite && state.lastRead != nil && state.lastRead.parentTx() != tx {
				op.AddDependency(state.lastRead)
			}

			if op.Access() == AccessTypeRead {
				state.lastRead = op
			}
			if op.Access() == AccessTypeWrite {
				state.lastWrite = op
			}

			states[op.Resource()] = state
		}
	}
}

// DebugDependencies returns a human friendly representation of the dependency graph.
func DebugDependencies(txs []*TxContext) string {
	var out string
	for _, tx := range txs {
		out += fmt.Sprintf("tx %s\n", tx.Name)
		for _, op := range tx.AccessOps {
			out += fmt.Sprintf("  - %-20s waits on %d ops\n", op.Name(), len(op.waitFor))
		}
	}
	return out
}
