package service

import (
	"sync"
	"sync/atomic"
)

type parallelScope struct {
	wg  sync.WaitGroup
	err atomic.Pointer[error]
}

// ParallelScope is a scope which doesn't require cancellation token,
// just parallelization.
type ParallelScope struct{ *parallelScope }

// Spawn spawns a new task in the scope.
func (s *parallelScope) Spawn(t func() error) {
	s.wg.Add(1)
	go func() {
		if err := t(); err != nil {
			s.err.CompareAndSwap(nil, &err)
		}
		s.wg.Done()
	}()
}

// Parallel executes a function in parallel scope.
// Compared to Run, it does not allow for early cancellation,
// therefore is suitable for non-blocking computations.
// Returns the first error returned by any of the spawned tasks.
// Waits until all the tasks complete, before returning.
func Parallel(main func(ParallelScope) error) error {
	var s parallelScope
	s.Spawn(func() error { return main(ParallelScope{&s}) })
	s.wg.Wait()
	if perr := s.err.Load(); perr != nil {
		return *perr
	}
	return nil
}
