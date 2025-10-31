package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"sync"
	"sync/atomic"
)

var (
	_ Service = (*BaseService)(nil)
)

// Service defines a service that can be started, stopped, and reset.
type Service interface {
	// Start is called to start the service, which should run until
	// the context terminates. If the service is already running, Start
	// must report an error.
	Start(context.Context) error

	// Manually terminates the service
	Stop()

	// Return true if the service is running
	IsRunning() bool

	// Wait blocks until the service is stopped.
	Wait()
}

// Implementation describes the implementation that the
// BaseService implementation wraps.
type Implementation interface {
	// Called by the Services Start Method
	OnStart(context.Context) error

	// Called when the service's context is canceled.
	OnStop()
}

type baseService struct {
	// This is the context that (structured concurrency) service tasks will be executed with.
	// It is canceled when outer context is canceled or when the service is stopped.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}
}

/*
Classical-inheritance-style service declarations. Services can be started, then
stopped, but cannot be restarted.

Users must implement OnStart/OnStop methods. In the absence of errors, these
methods are guaranteed to be called at most once. If OnStart returns an error,
service won't be marked as started, so the user can call Start again.

The BaseService implementation ensures that the OnStop method is
called after the context passed to Start is canceled.

Typical usage:

	type FooService struct {
		BaseService
		// private fields
	}

	func NewFooService() *FooService {
		fs := &FooService{
			// init
		}
		fs.BaseService = *NewBaseService(log, "FooService", fs)
		return fs
	}

	func (fs *FooService) OnStart(ctx context.Context) error {
		// initialize private fields
		// start subroutines, etc.
	}

	func (fs *FooService) OnStop() {
		// close/destroy private fields and releases resources
	}
*/
type BaseService struct {
	logger log.Logger
	name   string
	// The "subclass" of BaseService
	impl  Implementation
	inner atomic.Pointer[baseService]
}

// NewBaseService creates a new BaseService.
func NewBaseService(logger log.Logger, name string, impl Implementation) *BaseService {
	return &BaseService{
		logger: logger,
		name:   name,
		impl:   impl,
	}
}

// Start starts the Service and calls its OnStart method. An error
// will be returned if the service is stopped, but not if it is
// already running.
func (bs *BaseService) Start(ctx context.Context) error {
	sCtx, cancel := context.WithCancel(ctx)
	inner := &baseService{sCtx, cancel, sync.WaitGroup{}, make(chan struct{})}
	if !bs.inner.CompareAndSwap(nil, inner) {
		cancel() // free the context.
		return nil
	}

	bs.logger.Debug("starting service", "service", bs.name, "impl", bs.name)
	// Currently sei-tendermint services (and tests) rely on the fact that OnStart is called with
	// exactly the same context as Start.
	if err := bs.impl.OnStart(ctx); err != nil {
		cancel() // free the context.
		return err
	}

	go func() {
		<-inner.ctx.Done()
		inner.cancel() // free the context.
		bs.logger.Debug("stopping service", "service", bs.name)
		bs.impl.OnStop()
		inner.wg.Wait() // wait for all spawned tasks to finish
		bs.logger.Info("stopped service", "service", bs.name)
		close(inner.done)
	}()
	return nil
}

// Stop manually terminates the service by calling OnStop method from
// the implementation and releases all resources related to the
// service.
func (bs *BaseService) Stop() {
	if inner := bs.inner.Load(); inner != nil {
		inner.cancel()
		<-inner.done
	}
}

// Spawn spawns a new goroutine executing the task, which will be cancelled
// when outer context is cancelled or when the service is stopped.
// Error (other than ctx.Canceled) is logged after the task finishes.
// Both Wait and Stop calls will block until the spawned task is finished.
// It should be called ONLY from within OnStart().
// Note that the task is provided with a narrower context than the context
// provided to OnStart(). This is intentional.
// Panics if the service has not been started yet.
func (bs *BaseService) Spawn(name string, task func(ctx context.Context) error) {
	inner := bs.inner.Load()
	if inner == nil {
		panic("service is not started yet")
	}

	inner.wg.Add(1)
	go func() {
		defer inner.wg.Done()
		if err := utils.IgnoreCancel(task(inner.ctx)); err != nil {
			bs.logger.Error("task failed", "name", name, "service", bs.name, "error", err)
		}
	}()
}

// Spawns a critical task which should run until success OR as long as the service is running.
// It panics in any of the following cases:
// * task returns context.Canceled BEFORE the service is canceled.
// * task returns an error other than context.Canceled.
func (bs *BaseService) SpawnCritical(name string, task func(ctx context.Context) error) {
	inner := bs.inner.Load()
	if inner == nil {
		panic("service is not started yet")
	}

	inner.wg.Add(1)
	go func() {
		defer inner.wg.Done()
		if err := task(inner.ctx); err != nil {
			if !errors.Is(err, context.Canceled) || inner.ctx.Err() == nil {
				panic(fmt.Sprintf("critical task failed: name=%v, service=%v: %v", name, bs.name, err))
			}
		}
	}()
}

// IsRunning implements Service by returning true or false depending on the
// service's state.
func (bs *BaseService) IsRunning() bool {
	inner := bs.inner.Load()
	if inner == nil {
		return false
	}
	select {
	case <-inner.done:
		return false
	default:
		return true
	}
}

// Wait blocks until the service is stopped.
func (bs *BaseService) Wait() {
	if inner := bs.inner.Load(); inner != nil {
		<-inner.done
	}
}

// String provides a human-friendly representation of the service.
func (bs *BaseService) String() string { return bs.name }
