package evmonly

import (
	"context"
	"fmt"
	"sync"
)

type occWorkerPool struct {
	jobs   chan occPoolJob
	stop   chan struct{}
	closed chan struct{}
	once   sync.Once

	pinErrMu sync.Mutex
	pinErr   error
}

type occPoolJob struct {
	ctx     context.Context
	txRange occTxRange
	run     func(context.Context, occTxRange) error

	done    *sync.WaitGroup
	cancel  context.CancelFunc
	errOnce *sync.Once
	err     *error
}

func newOCCWorkerPool(workers int, pinWorkers bool, cpuOffset int) *occWorkerPool {
	p := &occWorkerPool{
		jobs:   make(chan occPoolJob, workers*2),
		stop:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	var workerWG sync.WaitGroup
	workerWG.Add(workers)
	for workerID := 0; workerID < workers; workerID++ {
		workerID := workerID
		go func() {
			defer workerWG.Done()
			p.runWorker(workerID, pinWorkers, cpuOffset)
		}()
	}
	go func() {
		workerWG.Wait()
		close(p.closed)
	}()
	return p
}

func (p *occWorkerPool) runWorker(workerID int, pinWorkers bool, cpuOffset int) {
	if pinWorkers {
		unlock, err := pinCurrentWorkerThread(cpuOffset + workerID)
		if err != nil {
			p.setPinErr(fmt.Errorf("pin OCC worker %d: %w", workerID, err))
		}
		defer unlock()
	}
	for {
		select {
		case <-p.stop:
			return
		case job := <-p.jobs:
			p.runJob(job)
		}
	}
}

func (p *occWorkerPool) runJob(job occPoolJob) {
	defer job.done.Done()
	if err := job.ctx.Err(); err != nil {
		return
	}
	if err := job.run(job.ctx, job.txRange); err != nil {
		job.errOnce.Do(func() {
			*job.err = err
			job.cancel()
		})
	}
}

func (p *occWorkerPool) Run(ctx context.Context, ranges []occTxRange, run func(context.Context, occTxRange) error) error {
	if err := p.getPinErr(); err != nil {
		return err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var done sync.WaitGroup
	var err error
	var errOnce sync.Once
dispatch:
	for _, txRange := range ranges {
		done.Add(1)
		job := occPoolJob{
			ctx:     jobCtx,
			txRange: txRange,
			run:     run,
			done:    &done,
			cancel:  cancel,
			errOnce: &errOnce,
			err:     &err,
		}
		select {
		case p.jobs <- job:
		case <-jobCtx.Done():
			done.Done()
			break dispatch
		case <-p.stop:
			done.Done()
			return fmt.Errorf("OCC worker pool is closed")
		}
	}
	done.Wait()
	if err != nil {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return nil
}

func (p *occWorkerPool) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		close(p.stop)
		<-p.closed
	})
}

func (p *occWorkerPool) setPinErr(err error) {
	p.pinErrMu.Lock()
	defer p.pinErrMu.Unlock()
	if p.pinErr == nil {
		p.pinErr = err
	}
}

func (p *occWorkerPool) getPinErr() error {
	p.pinErrMu.Lock()
	defer p.pinErrMu.Unlock()
	return p.pinErr
}
