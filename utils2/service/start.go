package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/utils2"
)

// Scope of concurrenct tasks.
type Scope struct {
	// scope is a concurrecy primitive, so no-ctx-in-struct rule does not apply
	// nolint:containedctx
	ctx  context.Context
	all  *errgroup.Group
	main *sync.WaitGroup
}

// Spawn spawns a main task.
// Scope gets automatically canceled when all the main tasks return.
func (s Scope) Spawn(t func() error) {
	s.main.Add(1)
	s.all.Go(func() error {
		defer s.main.Done()
		return t()
	})
}

// JoinHandle is a handle to an awaitable task.
type JoinHandle[R any] struct {
	result utils.AtomicRecv[*R]
}

// Spawn1 is the same as Scope.Spawn, but allows awaiting completion of a task and getting its result.
func Spawn1[R any](s Scope, t func() (R, error)) JoinHandle[R] {
	send := utils.NewAtomicSend[*R](nil)
	s.Spawn(func() error {
		v, err := t()
		if err != nil {
			return err
		}
		send.Send(&v)
		return nil
	})
	return JoinHandle[R]{send.Subscribe()}
}

// Join awaits completion of a task and returns its result.
// WARNING: it does NOT return the error of the task - error is returned from the Run() command.
// Join() can only fail when context is canceled.
func (h JoinHandle[R]) Join(ctx context.Context) (R, error) {
	res, err := h.result.Wait(ctx, func(v *R) bool { return v != nil })
	if err != nil {
		return utils.Zero[R](), err
	}
	return *res, nil
}

// If true, tasks that do not respect context cancellation will be logged.
// This is useful for debugging, but causes unnecessary overhead.
// Since this is a constant, debug guard should be optimized out by the compiler.
const enableDebugGuard = false

func (s Scope) debugGuard(name string, done chan struct{}) {
	select {
	case <-done:
		return
	case <-s.ctx.Done():
	}
	for {
		select {
		case <-done:
			return
		case <-time.After(10 * time.Second):
		}
		log.Printf("task %q still running", name)
	}
}

// SpawnNamed spawns a named main task.
func (s Scope) SpawnNamed(name string, t func() error) {
	done := make(chan struct{})
	s.Spawn(func() error {
		defer close(done)
		if err := t(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
	if enableDebugGuard {
		go s.debugGuard(name, done)
	}
}

// SpawnBgNamed spawns a named background task.
func (s Scope) SpawnBgNamed(name string, t func() error) {
	done := make(chan struct{})
	s.SpawnBg(func() error {
		defer close(done)
		if err := t(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
	if enableDebugGuard {
		go s.debugGuard(name, done)
	}
}

// SpawnBg spawns a background task.
// Background tasks get canceled when all the main tasks return.
func (s Scope) SpawnBg(t func() error) { s.all.Go(t) }

// Run runs a scope capable of spawning tasks.
// It is guaranteed that all the spawned tasks will be executed (even if spawned after the context is cancelled),
// and that `Run` will return only after all the tasks have completed.
// Context of the tasks will be automatically cancelled as soon as ANY task returns an error.
// Returns the first error returned by any task (main or background).
func Run(ctx context.Context, main func(context.Context, Scope) error) error {
	ctx, cancel := context.WithCancel(ctx)
	all, ctx := errgroup.WithContext(ctx)
	s := Scope{ctx, all, &sync.WaitGroup{}}
	s.Spawn(func() error { return main(ctx, s) })
	s.main.Wait()
	cancel()
	return s.all.Wait()
}

// Run1 is the same as Run, but returns the result of the main task.
func Run1[R any](ctx context.Context, main func(context.Context, Scope) (R, error)) (res R, err error) {
	err = Run(ctx, func(ctx context.Context, s Scope) error {
		var err error
		res, err = main(ctx, s)
		return err
	})
	//nolint:nakedret
	return
}
