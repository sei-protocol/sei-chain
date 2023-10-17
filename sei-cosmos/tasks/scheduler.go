package tasks

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/abci/types"
	"golang.org/x/sync/errgroup"
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
)

type deliverTxTask struct {
	Status      status
	Index       int
	Incarnation int
	Request     types.RequestDeliverTx
	Response    *types.ResponseDeliverTx
}

// Scheduler processes tasks concurrently
type Scheduler interface {
	ProcessAll(ctx sdk.Context, reqs []types.RequestDeliverTx) ([]types.ResponseDeliverTx, error)
}

type scheduler struct {
	deliverTx func(ctx sdk.Context, req types.RequestDeliverTx) (res types.ResponseDeliverTx)
	workers   int
}

// NewScheduler creates a new scheduler
func NewScheduler(workers int, deliverTxFunc func(ctx sdk.Context, req types.RequestDeliverTx) (res types.ResponseDeliverTx)) Scheduler {
	return &scheduler{
		workers:   workers,
		deliverTx: deliverTxFunc,
	}
}

func toTasks(reqs []types.RequestDeliverTx) []*deliverTxTask {
	res := make([]*deliverTxTask, 0, len(reqs))
	for idx, r := range reqs {
		res = append(res, &deliverTxTask{
			Request: r,
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

func (s *scheduler) ProcessAll(ctx sdk.Context, reqs []types.RequestDeliverTx) ([]types.ResponseDeliverTx, error) {
	tasks := toTasks(reqs)
	toExecute := tasks
	for len(toExecute) > 0 {

		// execute sets statuses of tasks to either executed or aborted
		err := s.executeAll(ctx, toExecute)
		if err != nil {
			return nil, err
		}

		// validate returns any that should be re-executed
		// note this processes ALL tasks, not just those recently executed
		toExecute, err = s.validateAll(ctx, tasks)
		if err != nil {
			return nil, err
		}
		for _, t := range toExecute {
			t.Incarnation++
			t.Status = statusPending
			t.Response = nil
			//TODO: reset anything that needs resetting
		}
	}
	return collectResponses(tasks), nil
}

// TODO: validate each tasks
// TODO: return list of tasks that are invalid
func (s *scheduler) validateAll(ctx sdk.Context, tasks []*deliverTxTask) ([]*deliverTxTask, error) {
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
		// any aborted tx is known to be suspect here
		if tasks[i].Status == statusAborted {
			res = append(res, tasks[i])
		} else {
			//TODO: validate the tasks and add it if invalid
			//TODO: create and handle abort for validation
			tasks[i].Status = statusValidated
		}
	}
	return res, nil
}

// ExecuteAll executes all tasks concurrently
// Tasks are updated with their status
// TODO: retries on aborted tasks
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
					//TODO: ensure version multi store is on context
					// buffered so it doesn't block on write
					// abortCh := make(chan occ.Abort, 1)

					//TODO: consume from abort in non-blocking way (give it a length)
					resp := s.deliverTx(ctx, task.Request)

					// close(abortCh)

					//if _, ok := <-abortCh; ok {
					//	tasks.status = TaskStatusAborted
					//	continue
					//}

					task.Status = statusExecuted
					task.Response = &resp
				}
			}
		})
	}
	grp.Go(func() error {
		defer close(ch)
		for _, task := range tasks {
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
