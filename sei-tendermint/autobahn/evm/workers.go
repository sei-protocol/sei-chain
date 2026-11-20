package evm

import (
	"context"
	"fmt"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// parseClaim is the number of transactions a worker takes per claim.
const parseClaim = 16

// ForEachTx calls fn for every index below n, with workers goroutines claiming parseClaim indexes
// at a time. It reports the first failure, wrapped with the index of the transaction that caused it.
func ForEachTx(ctx context.Context, n int, workers int, fn func(i int) error) error {
	if n == 0 {
		return nil
	}
	if workers <= 1 || n == 1 {
		for i := range n {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := fn(i); err != nil {
				return fmt.Errorf("parse tx %d: %w", i, err)
			}
		}
		return nil
	}
	workers = min(workers, n)

	g, groupCtx := errgroup.WithContext(ctx)
	var claimed atomic.Int64
	for range workers {
		g.Go(func() error {
			for {
				start := int(claimed.Add(parseClaim)) - parseClaim
				if start >= n {
					return nil
				}
				if err := groupCtx.Err(); err != nil {
					return err
				}
				for i := start; i < min(start+parseClaim, n); i++ {
					if err := fn(i); err != nil {
						return fmt.Errorf("parse tx %d: %w", i, err)
					}
				}
			}
		})
	}
	return g.Wait()
}
