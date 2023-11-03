package tasks

import (
	"sort"

	"github.com/tendermint/tendermint/abci/types"
	"golang.org/x/sync/errgroup"

	"github.com/cosmos/cosmos-sdk/store/multiversion"
	store "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/occ"
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
)

type deliverTxTask struct {
	Ctx     sdk.Context
	AbortCh chan occ.Abort

	Status        status
	Dependencies  []int
	Abort         *occ.Abort
	Index         int
	Incarnation   int
	Request       types.RequestDeliverTx
	Response      *types.ResponseDeliverTx
	VersionStores map[sdk.StoreKey]*multiversion.VersionIndexedStore
}

func (dt *deliverTxTask) Increment() {
	dt.Incarnation++
	dt.Status = statusPending
	dt.Response = nil
	dt.Abort = nil
	dt.AbortCh = nil
	dt.Dependencies = nil
	dt.VersionStores = nil
}

// Scheduler processes tasks concurrently
type Scheduler interface {
	ProcessAll(ctx sdk.Context, reqs []*sdk.DeliverTxEntry) ([]types.ResponseDeliverTx, error)
}

type scheduler struct {
	deliverTx          func(ctx sdk.Context, req types.RequestDeliverTx) (res types.ResponseDeliverTx)
	workers            int
	multiVersionStores map[sdk.StoreKey]multiversion.MultiVersionStore
}

// NewScheduler creates a new scheduler
func NewScheduler(workers int, deliverTxFunc func(ctx sdk.Context, req types.RequestDeliverTx) (res types.ResponseDeliverTx)) Scheduler {
	return &scheduler{
		workers:   workers,
		deliverTx: deliverTxFunc,
	}
}

func (s *scheduler) invalidateTask(task *deliverTxTask) {
	for _, mv := range s.multiVersionStores {
		mv.InvalidateWriteset(task.Index, task.Incarnation)
	}
}

func (s *scheduler) findConflicts(task *deliverTxTask) (bool, []int) {
	var conflicts []int
	uniq := make(map[int]struct{})
	valid := true
	for _, mv := range s.multiVersionStores {
		ok, mvConflicts := mv.ValidateTransactionState(task.Index)
		for _, c := range mvConflicts {
			if _, ok := uniq[c]; !ok {
				conflicts = append(conflicts, c)
				uniq[c] = struct{}{}
			}
		}
		// any non-ok value makes valid false
		valid = ok && valid
	}
	sort.Ints(conflicts)
	return valid, conflicts
}

func toTasks(reqs []*sdk.DeliverTxEntry) []*deliverTxTask {
	res := make([]*deliverTxTask, 0, len(reqs))
	for idx, r := range reqs {
		res = append(res, &deliverTxTask{
			Request: r.Request,
			Index:   idx,
			Status:  statusPending,
		})
	}
	return res
}

func collectResponses(tasks []*deliverTxTask) []types.ResponseDeliverTx {
	res := make([]types.ResponseDeliverTx, 0, len(tasks))
	for _, t := range tasks {
		res = append(res, *t.Response)
	}
	return res
}

func (s *scheduler) tryInitMultiVersionStore(ctx sdk.Context) {
	if s.multiVersionStores != nil {
		return
	}
	mvs := make(map[sdk.StoreKey]multiversion.MultiVersionStore)
	keys := ctx.MultiStore().StoreKeys()
	for _, sk := range keys {
		mvs[sk] = multiversion.NewMultiVersionStore(ctx.MultiStore().GetKVStore(sk))
	}
	s.multiVersionStores = mvs
}

func indexesValidated(tasks []*deliverTxTask, idx []int) bool {
	for _, i := range idx {
		if tasks[i].Status != statusValidated {
			return false
		}
	}
	return true
}

func allValidated(tasks []*deliverTxTask) bool {
	for _, t := range tasks {
		if t.Status != statusValidated {
			return false
		}
	}
	return true
}

func (s *scheduler) PrefillEstimates(ctx sdk.Context, reqs []*sdk.DeliverTxEntry) {
	// iterate over TXs, update estimated writesets where applicable
	for i, req := range reqs {
		mappedWritesets := req.EstimatedWritesets
		// order shouldnt matter for storeKeys because each storeKey partitioned MVS is independent
		for storeKey, writeset := range mappedWritesets {
			// we use `-1` to indicate a prefill incarnation
			s.multiVersionStores[storeKey].SetEstimatedWriteset(i, -1, writeset)
		}
	}
}

func (s *scheduler) ProcessAll(ctx sdk.Context, reqs []*sdk.DeliverTxEntry) ([]types.ResponseDeliverTx, error) {
	// initialize mutli-version stores if they haven't been initialized yet
	s.tryInitMultiVersionStore(ctx)
	// prefill estimates
	s.PrefillEstimates(ctx, reqs)
	tasks := toTasks(reqs)
	toExecute := tasks
	for !allValidated(tasks) {
		var err error

		// execute sets statuses of tasks to either executed or aborted
		if len(toExecute) > 0 {
			err = s.executeAll(ctx, toExecute)
			if err != nil {
				return nil, err
			}
		}

		// validate returns any that should be re-executed
		// note this processes ALL tasks, not just those recently executed
		toExecute, err = s.validateAll(tasks)
		if err != nil {
			return nil, err
		}
		for _, t := range toExecute {
			t.Increment()
		}
	}
	for _, mv := range s.multiVersionStores {
		mv.WriteLatestToStore()
	}
	return collectResponses(tasks), nil
}

func (s *scheduler) validateAll(tasks []*deliverTxTask) ([]*deliverTxTask, error) {
	var res []*deliverTxTask

	// find first non-validated entry
	var startIdx int
	for idx, t := range tasks {
		if t.Status != statusValidated {
			startIdx = idx
			break
		}
	}

	for i := startIdx; i < len(tasks); i++ {
		switch tasks[i].Status {
		case statusAborted:
			// aborted means it can be re-run immediately
			res = append(res, tasks[i])

		// validated tasks can become unvalidated if an earlier re-run task now conflicts
		case statusExecuted, statusValidated:
			if valid, conflicts := s.findConflicts(tasks[i]); !valid {
				s.invalidateTask(tasks[i])

				// if the conflicts are now validated, then rerun this task
				if indexesValidated(tasks, conflicts) {
					res = append(res, tasks[i])
				} else {
					// otherwise, wait for completion
					tasks[i].Dependencies = conflicts
					tasks[i].Status = statusWaiting
				}
			} else if len(conflicts) == 0 {
				tasks[i].Status = statusValidated
			} // TODO: do we need to have handling for conflicts existing here?

		case statusWaiting:
			// if conflicts are done, then this task is ready to run again
			if indexesValidated(tasks, tasks[i].Dependencies) {
				res = append(res, tasks[i])
			}
		}
	}
	return res, nil
}

// ExecuteAll executes all tasks concurrently
// Tasks are updated with their status
// TODO: error scenarios
func (s *scheduler) executeAll(ctx sdk.Context, tasks []*deliverTxTask) error {
	ch := make(chan *deliverTxTask, len(tasks))
	grp, gCtx := errgroup.WithContext(ctx.Context())

	// a workers value < 1 means no limit
	workers := s.workers
	if s.workers < 1 {
		workers = len(tasks)
	}

	for i := 0; i < workers; i++ {
		grp.Go(func() error {
			for {
				select {
				case <-gCtx.Done():
					return gCtx.Err()
				case task, ok := <-ch:
					if !ok {
						return nil
					}

					resp := s.deliverTx(task.Ctx, task.Request)

					close(task.AbortCh)

					if abt, ok := <-task.AbortCh; ok {
						task.Status = statusAborted
						task.Abort = &abt
						continue
					}

					// write from version store to multiversion stores
					for _, v := range task.VersionStores {
						v.WriteToMultiVersionStore()
					}

					task.Status = statusExecuted
					task.Response = &resp
				}
			}
		})
	}
	grp.Go(func() error {
		defer close(ch)
		for _, task := range tasks {
			// initialize the context
			ctx = ctx.WithTxIndex(task.Index)
			abortCh := make(chan occ.Abort, len(s.multiVersionStores))

			// if there are no stores, don't try to wrap, because there's nothing to wrap
			if len(s.multiVersionStores) > 0 {
				// non-blocking
				cms := ctx.MultiStore().CacheMultiStore()

				// init version stores by store key
				vs := make(map[store.StoreKey]*multiversion.VersionIndexedStore)
				for storeKey, mvs := range s.multiVersionStores {
					vs[storeKey] = mvs.VersionedIndexedStore(task.Index, task.Incarnation, abortCh)
				}

				// save off version store so we can ask it things later
				task.VersionStores = vs
				ms := cms.SetKVStores(func(k store.StoreKey, kvs sdk.KVStore) store.CacheWrap {
					return vs[k]
				})

				ctx = ctx.WithMultiStore(ms)
			}

			task.AbortCh = abortCh
			task.Ctx = ctx

			select {
			case <-gCtx.Done():
				return gCtx.Err()
			case ch <- task:
			}
		}
		return nil
	})

	if err := grp.Wait(); err != nil {
		return err
	}

	return nil
}
