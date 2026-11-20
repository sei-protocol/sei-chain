package evm

import (
	"fmt"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// claimSize is the number of transactions a worker takes per claim.
const claimSize = 16

// forEach calls fn for every index below n, with workers goroutines claiming claimSize indexes
// at a time. It reports the first failure, wrapped with the index of the transaction that caused it.
func forEach(n int, workers int, fn func(i int) error) error {
	if n == 0 {
		return nil
	}
	if workers <= 1 || n == 1 {
		for i := range n {
			if err := fn(i); err != nil {
				return fmt.Errorf("parse tx %d: %w", i, err)
			}
		}
		return nil
	}
	workers = min(workers, n)

	var claimed atomic.Int64
	return scope.Parallel(func(s scope.ParallelScope) error {
		for range workers {
			s.Spawn(func() error {
				for {
					start := int(claimed.Add(claimSize)) - claimSize
					if start >= n {
						return nil
					}
					for i := start; i < min(start+claimSize, n); i++ {
						if err := fn(i); err != nil {
							return fmt.Errorf("parse tx %d: %w", i, err)
						}
					}
				}
			})
		}
		return nil
	})
}
