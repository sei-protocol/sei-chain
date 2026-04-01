package threading

// Pool is a pool of workers that can be used to execute tasks concurrently.
type Pool interface {
	// Submit submits a task to the pool. The task must not be nil.

	// Although it is thread safe to call Submit() from concurrent goroutines, it is not thread safe to call
	// Submit and Close() concurrently.
	Submit(task func())

	// Close shuts down the pool, draining any buffered tasks, and blocks until all workers have exited.
	//
	// Safe to call multiple times (idempotent). Not safe to call concurrently with Submit().
	Close()
}
