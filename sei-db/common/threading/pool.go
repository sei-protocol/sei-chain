package threading

import "context"

// Pool is a pool of workers that can be used to execute tasks concurrently.
type Pool interface {
	// Submit submits a task to the pool. The task must not be nil.
	//
	// If Submit is called concurrently with or after shutdown (i.e. when ctx is done/cancelled), the task may
	// be silently dropped. Callers that need a guarantee of execution must
	// ensure Submit happens-before shutdown.
	Submit(ctx context.Context, task func()) error
}
