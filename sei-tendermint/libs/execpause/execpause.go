// Package execpause holds an optional CheckTx gate. With the environment
// variable unset, every function is a no-op.
//
//	SEI_PAUSE_CHECKTX_MIN  while a block of at least this many txs executes,
//	                       CheckTx waits so it does not share the cores
package execpause

import (
	"context"
	"os"
	"strconv"
	"sync"
)

var (
	cfgOnce sync.Once
	cfg     int

	mu    sync.Mutex
	cond  = sync.NewCond(&mu)
	depth int
)

func pauseMin() int {
	cfgOnce.Do(func() {
		v := os.Getenv("SEI_PAUSE_CHECKTX_MIN")
		if v == "" {
			return
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return
		}
		cfg = n
	})
	return cfg
}

// Acquire pauses CheckTx while a large block executes. The caller must
// Release if Acquire returns true.
func Acquire(txs int) bool {
	min := pauseMin()
	if min <= 0 || txs < min {
		return false
	}
	mu.Lock()
	depth++
	mu.Unlock()
	return true
}

// Release ends the matching Acquire.
func Release() {
	mu.Lock()
	if depth > 0 {
		depth--
	}
	if depth == 0 {
		cond.Broadcast()
	}
	mu.Unlock()
}

// Wait blocks CheckTx until no large block is executing.
func Wait(ctx context.Context) error {
	if pauseMin() <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	for depth > 0 {
		cond.Wait()
	}
	return nil
}
