package threading

import "context"

// Pool is a pool of workers that can be used to execute tasks concurrently.
type Pool interface {
	// Submit submits a task to the pool. The task must not be nil.
	//
	// If Submit is called concurrently with or after shutdown (i.e. when ctx is done/cancelled), the task may
	// be silently dropped. Callers that need a guarantee of execution must
	// ensure Submit happens-before shutdown.
	//
	// This method is permitted to return an error only under the following conditions:
	// - the pool is shutting down (i.e. its context is done/cancelled)
	// - the provided ctx parameter is done/cancelled before this method returns
	// - invalid input (e.g. the task is nil)
	//
	// If this method returns an error, the task may or may not have been executed.
	Submit(ctx context.Context, task func()) error
}
