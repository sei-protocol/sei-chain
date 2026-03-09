package threading

import "context"

// Pool is a pool of workers that can be used to execute tasks concurrently.
type Pool interface {
	// Submit submits a task to the pool.
	Submit(ctx context.Context, task func()) error
}
