package tasks

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/store/multiversion"
	store "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/occ"
	"github.com/cosmos/cosmos-sdk/utils/tracing"
	"github.com/tendermint/tendermint/abci/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type status string

const (
	// statusPending tasks are ready for execution
	// all executing tasks are in pending state
	statusPending status = "pending"
	// statusExecuted tasks are ready for validation
	// these tasks did not abort during execution
	statusExecuted status = "executed"
	// statusAborted means the task has been aborted
	// these tasks transition to pending upon next execution
	statusAborted status = "aborted"
	// statusValidated means the task has been validated
	// tasks in this status can be reset if an earlier task fails validation
	statusValidated status = "validated"
	// statusWaiting tasks are waiting for another tx to complete
	statusWaiting status = "waiting"
	// maximumIterations before we revert to sequential (for high conflict rates)
	maximumIterations = 10
)

var _ SchedulerTask[*types.ResponseDeliverTx] = (*DeliverTxTask)(nil)

type DeliverTxTask struct {
	Ctx     sdk.Context
	abortCh chan occ.Abort

	mx            sync.RWMutex
	status        status
	dependencies  map[int]struct{}
	Abort         *occ.Abort
	incarnation   int
	Request       types.RequestDeliverTxV2
	SdkTx         sdk.Tx
	Checksum      [32]byte
	absoluteIndex int
	response      *types.ResponseDeliverTx
	versionStores map[string]*multiversion.VersionIndexedStore
	txTracer      sdk.TxTracer
}

// AppendDependencies appends the given indexes to the task's dependencies
func (dt *DeliverTxTask) AppendDependencies(deps []int) {
	dt.mx.Lock()
	defer dt.mx.Unlock()
	for _, taskIdx := range deps {
		dt.dependencies[taskIdx] = struct{}{}
	}
}

func (dt *DeliverTxTask) IsStatus(s status) bool {
	dt.mx.RLock()
	defer dt.mx.RUnlock()
	return dt.status == s
}

func (dt *DeliverTxTask) SetStatus(s status) {
	dt.mx.Lock()
	defer dt.mx.Unlock()
	dt.status = s
}

func (dt *DeliverTxTask) Status() status {
	return dt.status
}

func (dt *DeliverTxTask) Reset() {
	dt.SetStatus(statusPending)
	dt.response = nil
	dt.Abort = nil
	dt.abortCh = nil
	dt.versionStores = nil

	if dt.txTracer != nil {
		dt.txTracer.Reset()
	}
}

func (dt *DeliverTxTask) Increment() {
	dt.incarnation++
}

func (dt *DeliverTxTask) Prepare(mvs map[string]multiversion.MultiVersionStore) {
	ctx := dt.Ctx.WithTxIndex(dt.absoluteIndex)

	// initialize the context
	abortCh := make(chan occ.Abort, len(mvs))

	// if there are no stores, don't try to wrap, because there's nothing to wrap
	if len(mvs) > 0 {
		// non-blocking
		cms := ctx.MultiStore().CacheMultiStore()

		// init version stores by store key
		vs := make(map[string]*multiversion.VersionIndexedStore)
		for storeKey, mvs := range mvs {
			vs[storeKey] = mvs.VersionedIndexedStore(dt.absoluteIndex, dt.incarnation, abortCh)
		}

		// save off version store so we can ask it things later
		dt.versionStores = vs
		ms := cms.SetKVStores(func(k store.StoreKey, kvs sdk.KVStore) store.CacheWrap {
			return vs[k.String()]
		})

		ctx = ctx.WithMultiStore(ms)
	}

	if dt.txTracer != nil {
		ctx = dt.txTracer.InjectInContext(ctx)
	}

	dt.abortCh = abortCh
	dt.Ctx = ctx
}

func (dt *DeliverTxTask) AbortCh() chan occ.Abort {
	return dt.abortCh
}

func (dt *DeliverTxTask) SetAbort(abort *occ.Abort) {
	dt.Abort = abort
}

func (dt *DeliverTxTask) VersionStores() map[string]*multiversion.VersionIndexedStore {
	return dt.versionStores
}

func (dt *DeliverTxTask) SetResponse(response *types.ResponseDeliverTx) {
	dt.response = response
}

func (dt *DeliverTxTask) TxTracer() sdk.TxTracer {
	return dt.txTracer
}

func (dt *DeliverTxTask) SetTxTracer(txTracer sdk.TxTracer) {
	dt.txTracer = txTracer
}

func (dt *DeliverTxTask) Tx() []byte {
	return dt.Request.Tx
}

func (dt *DeliverTxTask) AbsoluteIndex() int {
	return dt.absoluteIndex
}

func (dt *DeliverTxTask) SetAbsoluteIndex(absoluteIndex int) {
	dt.absoluteIndex = absoluteIndex
}

func (dt *DeliverTxTask) Incarnation() int {
	return dt.incarnation
}

func (dt *DeliverTxTask) Response() *types.ResponseDeliverTx {
	return dt.response
}

func (dt *DeliverTxTask) Dependencies() map[int]struct{} {
	return dt.dependencies
}

func (dt *DeliverTxTask) SetDependencies(dependencies map[int]struct{}) {
	dt.dependencies = dependencies
}

func (dt *DeliverTxTask) SetTracerCtx(ctx context.Context) {
	dt.Ctx = dt.Ctx.WithContext(ctx)
}

func (dt *DeliverTxTask) TracerCtx() context.Context {
	return dt.Ctx.Context()
}

type Stats []interface{}

// Scheduler processes tasks concurrently
type Scheduler[Response any] interface {
	ProcessAll(
		ctx context.Context,
		tasks []SchedulerTask[Response],
		mvs map[string]multiversion.MultiVersionStore,
	) ([]Response, Stats, error)
}

type SchedulerTask[Response any] interface {
	Tx() []byte
	AbsoluteIndex() int
	Incarnation() int
	Response() Response
	IsStatus(status) bool
	Status() status
	TracerCtx() context.Context
	SetTracerCtx(context.Context)
	Reset()
	Increment()
	Prepare(map[string]multiversion.MultiVersionStore)
	AbortCh() chan occ.Abort
	SetAbort(*occ.Abort)
	AppendDependencies(deps []int)
	Dependencies() map[int]struct{}
	SetStatus(s status)
	VersionStores() map[string]*multiversion.VersionIndexedStore
	SetResponse(Response)

	TxTracer() sdk.TxTracer // to be deprecated
}

var _ Scheduler[any] = (*scheduler[any])(nil)

type scheduler[Response any] struct {
	// deliverTx          func(ctx sdk.Context, req types.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) (res types.ResponseDeliverTx)
	executor           func(SchedulerTask[Response]) Response
	workers            int
	multiVersionStores map[string]multiversion.MultiVersionStore
	tracingInfo        *tracing.Info
	allTasksMap        map[int]SchedulerTask[Response]
	allTasks           []SchedulerTask[Response]
	executeCh          chan func()
	validateCh         chan func()
	metrics            *schedulerMetrics
	synchronous        bool // true if maxIncarnation exceeds threshold
	maxIncarnation     int  // current highest incarnation
}

// NewScheduler creates a new scheduler
func NewScheduler[Response any](workers int, tracingInfo *tracing.Info, executor func(SchedulerTask[Response]) Response) Scheduler[Response] {
	return &scheduler[Response]{
		workers:     workers,
		executor:    executor,
		tracingInfo: tracingInfo,
		metrics:     &schedulerMetrics{},
	}
}

func (s *scheduler[Response]) invalidateTask(task SchedulerTask[Response]) {
	for _, mv := range s.multiVersionStores {
		mv.InvalidateWriteset(task.AbsoluteIndex(), task.Incarnation())
		mv.ClearReadset(task.AbsoluteIndex())
		mv.ClearIterateset(task.AbsoluteIndex())
	}
}

func start(ctx context.Context, ch chan func(), workers int) {
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case work := <-ch:
					work()
				}
			}
		}()
	}
}

func (s *scheduler[Response]) DoValidate(work func()) {
	if s.synchronous {
		work()
		return
	}
	s.validateCh <- work
}

func (s *scheduler[Response]) DoExecute(work func()) {
	if s.synchronous {
		work()
		return
	}
	s.executeCh <- work
}

func (s *scheduler[Response]) findConflicts(task SchedulerTask[Response]) (bool, []int) {
	var conflicts []int
	uniq := make(map[int]struct{})
	valid := true
	for _, mv := range s.multiVersionStores {
		ok, mvConflicts := mv.ValidateTransactionState(task.AbsoluteIndex())
		for _, c := range mvConflicts {
			if _, ok := uniq[c]; !ok {
				conflicts = append(conflicts, c)
				uniq[c] = struct{}{}
			}
		}
		// any non-ok value makes valid false
		valid = valid && ok
	}
	sort.Ints(conflicts)
	return valid, conflicts
}

func (s *scheduler[Response]) collectResponses(tasks []SchedulerTask[Response]) []Response {
	res := make([]Response, 0, len(tasks))
	for _, t := range tasks {
		res = append(res, t.Response())

		if t.TxTracer != nil {
			t.TxTracer().Commit()
		}
	}
	return res
}

func (s *scheduler[Response]) tryInitMultiVersionStore(ctx sdk.Context) {
	if s.multiVersionStores != nil {
		return
	}
	mvs := make(map[string]multiversion.MultiVersionStore)
	keys := ctx.MultiStore().StoreKeys()
	for _, sk := range keys {
		mvs[sk.String()] = multiversion.NewMultiVersionStore(ctx.MultiStore().GetKVStore(sk))
	}
	s.multiVersionStores = mvs
}

func dependenciesValidated[Response any](tasksMap map[int]SchedulerTask[Response], deps map[int]struct{}) bool {
	for i := range deps {
		// because idx contains absoluteIndices, we need to fetch from map
		task := tasksMap[i]
		if !task.IsStatus(statusValidated) {
			return false
		}
	}
	return true
}

func allValidated[Response any](tasks []SchedulerTask[Response]) bool {
	for _, t := range tasks {
		if !t.IsStatus(statusValidated) {
			return false
		}
	}
	return true
}

// schedulerMetrics contains metrics for the scheduler
type schedulerMetrics struct {
	// maxIncarnation is the highest incarnation seen in this set
	maxIncarnation int
	// retries is the number of tx attempts beyond the first attempt
	retries int
}

func (s *scheduler[Response]) emitMetrics() {
	telemetry.IncrCounter(float32(s.metrics.retries), "scheduler", "retries")
	telemetry.IncrCounter(float32(s.metrics.maxIncarnation), "scheduler", "incarnations")
}

func (s *scheduler[Response]) ProcessAll(
	ctx context.Context,
	tasks []SchedulerTask[Response],
	mvs map[string]multiversion.MultiVersionStore,
) ([]Response, Stats, error) {
	startTime := time.Now()
	var iterations int
	// initialize mutli-version stores if they haven't been initialized yet
	s.multiVersionStores = mvs
	s.allTasks = tasks
	s.allTasksMap = make(map[int]SchedulerTask[Response], len(tasks))
	for _, t := range tasks {
		s.allTasksMap[t.AbsoluteIndex()] = t
	}
	s.executeCh = make(chan func(), len(tasks))
	s.validateCh = make(chan func(), len(tasks))
	defer s.emitMetrics()

	// default to number of tasks if workers is negative or 0 by this point
	workers := s.workers
	if s.workers < 1 || len(tasks) < s.workers {
		workers = len(tasks)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// execution tasks are limited by workers
	start(workerCtx, s.executeCh, workers)

	// validation tasks uses length of tasks to avoid blocking on validation
	start(workerCtx, s.validateCh, len(tasks))

	toExecute := tasks
	for !allValidated(tasks) {
		// if the max incarnation >= x, we should revert to synchronous
		if iterations >= maximumIterations {
			// process synchronously
			s.synchronous = true
			startIdx, anyLeft := s.findFirstNonValidated()
			if !anyLeft {
				break
			}
			toExecute = tasks[startIdx:]
		}

		// execute sets statuses of tasks to either executed or aborted
		if err := s.executeAll(ctx, toExecute); err != nil {
			return nil, nil, err
		}

		// validate returns any that should be re-executed
		// note this processes ALL tasks, not just those recently executed
		var err error
		toExecute, err = s.validateAll(ctx, tasks)
		if err != nil {
			return nil, nil, err
		}
		// these are retries which apply to metrics
		s.metrics.retries += len(toExecute)
		iterations++
	}

	for _, mv := range s.multiVersionStores {
		mv.WriteLatestToStore()
	}
	s.metrics.maxIncarnation = s.maxIncarnation

	return s.collectResponses(tasks), Stats{
		"txs", len(tasks),
		"latency_ms", time.Since(startTime).Milliseconds(),
		"retries", s.metrics.retries,
		"maxIncarnation", s.maxIncarnation,
		"iterations", iterations,
		"sync", s.synchronous,
		"workers", s.workers}, nil
}

func (s *scheduler[Response]) shouldRerun(task SchedulerTask[Response]) bool {
	switch task.Status() {

	case statusAborted, statusPending:
		return true

	// validated tasks can become unvalidated if an earlier re-run task now conflicts
	case statusExecuted, statusValidated:
		// With the current scheduler, we won't actually get to this step if a previous task has already been determined to be invalid,
		// since we choose to fail fast and mark the subsequent tasks as invalid as well.
		// TODO: in a future async scheduler that no longer exhaustively validates in order, we may need to carefully handle the `valid=true` with conflicts case
		if valid, conflicts := s.findConflicts(task); !valid {
			s.invalidateTask(task)
			task.AppendDependencies(conflicts)

			// if the conflicts are now validated, then rerun this task
			if dependenciesValidated(s.allTasksMap, task.Dependencies()) {
				return true
			} else {
				// otherwise, wait for completion
				task.SetStatus(statusWaiting)
				return false
			}
		} else if len(conflicts) == 0 {
			// mark as validated, which will avoid re-validating unless a lower-index re-validates
			task.SetStatus(statusValidated)
			return false
		}
		// conflicts and valid, so it'll validate next time
		return false

	case statusWaiting:
		// if conflicts are done, then this task is ready to run again
		return dependenciesValidated(s.allTasksMap, task.Dependencies())
	}
	panic("unexpected status: " + task.Status())
}

func (s *scheduler[Response]) validateTask(ctx context.Context, task SchedulerTask[Response]) bool {
	_, span := s.traceSpan(ctx, "SchedulerValidate", task)
	defer span.End()

	if s.shouldRerun(task) {
		return false
	}
	return true
}

func (s *scheduler[Response]) findFirstNonValidated() (int, bool) {
	for i, t := range s.allTasks {
		if t.Status() != statusValidated {
			return i, true
		}
	}
	return 0, false
}

func (s *scheduler[Response]) validateAll(ctx context.Context, tasks []SchedulerTask[Response]) ([]SchedulerTask[Response], error) {
	ctx, span := s.traceSpan(ctx, "SchedulerValidateAll", nil)
	defer span.End()

	var mx sync.Mutex
	var res []SchedulerTask[Response]

	startIdx, anyLeft := s.findFirstNonValidated()

	if !anyLeft {
		return nil, nil
	}

	wg := &sync.WaitGroup{}
	for i := startIdx; i < len(tasks); i++ {
		wg.Add(1)
		t := tasks[i]
		s.DoValidate(func() {
			defer wg.Done()
			if !s.validateTask(ctx, t) {
				mx.Lock()
				defer mx.Unlock()
				t.Reset()
				t.Increment()
				// update max incarnation for scheduler
				if t.Incarnation() > s.maxIncarnation {
					s.maxIncarnation = t.Incarnation()
				}
				res = append(res, t)
			}
		})
	}
	wg.Wait()

	return res, nil
}

// ExecuteAll executes all tasks concurrently
func (s *scheduler[Response]) executeAll(ctx context.Context, tasks []SchedulerTask[Response]) error {
	if len(tasks) == 0 {
		return nil
	}
	ctx, span := s.traceSpan(ctx, "SchedulerExecuteAll", nil)
	span.SetAttributes(attribute.Bool("synchronous", s.synchronous))
	defer span.End()

	// validationWg waits for all validations to complete
	// validations happen in separate goroutines in order to wait on previous index
	wg := &sync.WaitGroup{}
	wg.Add(len(tasks))

	for _, task := range tasks {
		t := task
		s.DoExecute(func() {
			s.prepareAndRunTask(wg, ctx, t)
		})
	}

	wg.Wait()

	return nil
}

func (s *scheduler[Response]) prepareAndRunTask(wg *sync.WaitGroup, ctx context.Context, task SchedulerTask[Response]) {
	eCtx, eSpan := s.traceSpan(ctx, "SchedulerExecute", task)
	defer eSpan.End()

	task.SetTracerCtx(eCtx)
	s.executeTask(task)
	wg.Done()
}

func (s *scheduler[Response]) traceSpan(ctx context.Context, name string, task SchedulerTask[Response]) (context.Context, trace.Span) {
	spanCtx, span := s.tracingInfo.StartWithContext(name, ctx)
	if task != nil {
		span.SetAttributes(attribute.String("txHash", fmt.Sprintf("%X", sha256.Sum256(task.Tx()))))
		span.SetAttributes(attribute.Int("absoluteIndex", task.AbsoluteIndex()))
		span.SetAttributes(attribute.Int("txIncarnation", task.Incarnation()))
	}
	return spanCtx, span
}

func (s *scheduler[Response]) executeTask(task SchedulerTask[Response]) {
	dCtx, dSpan := s.traceSpan(task.TracerCtx(), "SchedulerExecuteTask", task)
	defer dSpan.End()
	task.SetTracerCtx(dCtx)

	// in the synchronous case, we only want to re-execute tasks that need re-executing
	if s.synchronous {
		// even if already validated, it could become invalid again due to preceeding
		// reruns. Make sure previous writes are invalidated before rerunning.
		if task.IsStatus(statusValidated) {
			s.invalidateTask(task)
		}

		// waiting transactions may not yet have been reset
		// this ensures a task has been reset and incremented
		if !task.IsStatus(statusPending) {
			task.Reset()
			task.Increment()
		}
	}

	task.Prepare(s.multiVersionStores)

	resp := s.executor(task)
	// close the abort channel
	close(task.AbortCh())
	abort, ok := <-task.AbortCh()
	if ok {
		// if there is an abort item that means we need to wait on the dependent tx
		task.SetStatus(statusAborted)
		task.SetAbort(&abort)
		task.AppendDependencies([]int{abort.DependentTxIdx})
		// write from version store to multiversion stores
		for _, v := range task.VersionStores() {
			v.WriteEstimatesToMultiVersionStore()
		}
		return
	}

	task.SetStatus(statusExecuted)
	task.SetResponse(resp)

	// write from version store to multiversion stores
	for _, v := range task.VersionStores() {
		v.WriteToMultiVersionStore()
	}
}
