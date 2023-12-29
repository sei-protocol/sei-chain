package contract

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/utils/logging"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const LogAfter = 10 * time.Second

type ParallelRunner struct {
	runnable func(contract types.ContractInfoV2)

	contractAddrToInfo   *datastructures.TypedSyncMap[types.ContractAddress, *types.ContractInfoV2]
	readyContracts       *datastructures.TypedSyncMap[types.ContractAddress, struct{}]
	readyCnt             int64
	inProgressCnt        int64
	someContractFinished chan struct{}
	done                 chan struct{}
	sdkCtx               sdk.Context
}

func NewParallelRunner(runnable func(contract types.ContractInfoV2), contracts []types.ContractInfoV2, ctx sdk.Context) ParallelRunner {
	contractAddrToInfo := datastructures.NewTypedSyncMap[types.ContractAddress, *types.ContractInfoV2]()
	contractsFrontier := datastructures.NewTypedSyncMap[types.ContractAddress, struct{}]()
	for _, contract := range contracts {
		// runner will mutate ContractInfo fields
		copyContract := contract
		typedContractAddr := types.ContractAddress(contract.ContractAddr)
		contractAddrToInfo.Store(typedContractAddr, &copyContract)
		if copyContract.NumIncomingDependencies == 0 {
			contractsFrontier.Store(typedContractAddr, struct{}{})
		}
	}
	return ParallelRunner{
		runnable:             runnable,
		contractAddrToInfo:   contractAddrToInfo,
		readyContracts:       contractsFrontier,
		readyCnt:             int64(contractsFrontier.Len()),
		inProgressCnt:        0,
		someContractFinished: make(chan struct{}),
		done:                 make(chan struct{}, 1),
		sdkCtx:               ctx,
	}
}

// We define "frontier contract" as a contract which:
//  1. Has not finished running yet, and
//  2. either:
//     a. has no other contracts depending on it, or
//     b. for which all contracts that depend on it have already finished.
//
// Consequently, the set of frontier contracts will mutate throughout the
// `Run` method, until all contracts finish their runs.
// The key principle here is that at any moment, we can have all frontier
// contracts running concurrently, since there must be no ancestral
// relationships among them due to the definition above.
// The simplest implementation would be:
// ```
// while there is any contract left:
//
//	run all frontier contracts concurrently
//	wait for all runs to finish
//	update the frontier set
//
// ```
// We can illustrate why this implementation is not optimal with the following
// example:
//
//	Suppose we have four contracts, where A depends on B, and C depends on
//	D. The run for A, B, C, D takes 5s, 5s, 8s, 2s, respectively.
//	With the implementation above, the first iteration would take 8s since
//	it runs A and C, and the second iteration would take 5s since it runs
//	B and D. However C doesn't actually need to wait for B to finish, and
//	if C runs immediately after A finishes, the whole process would take
//	max(5 + 5, 8 + 2) = 10s, which is 3s faster than the implementation
//	above.
//
// So we can optimize the implementation to be:
// ```
// while there is any contract left:
//
//	run all frontier contracts concurrently
//	wait for any existing run (could be from previous iteration) to finish
//	update the frontier set
//
// ```
// With the example above, the whole process would take 3 iterations:
// Iter 1 (A, C run): 5s since it finishes when A finishes
// Iter 2 (B run): 3s since it finishes when C finishes
// Iter 3 (D run): 2s since it finishes when B, D finish
//
// The following `Run` method implements the pseudocode above.
func (r *ParallelRunner) Run() {
	if atomic.LoadInt64(&r.inProgressCnt) == 0 && atomic.LoadInt64(&r.readyCnt) == 0 {
		return
	}
	// The ordering of the two conditions below matters, since readyCnt
	// is updated before inProgressCnt.
	for atomic.LoadInt64(&r.inProgressCnt) > 0 || atomic.LoadInt64(&r.readyCnt) > 0 {
		// r.readyContracts represent all frontier contracts that have
		// not started running yet.
		r.readyContracts.Range(func(key types.ContractAddress, _ struct{}) bool {
			atomic.AddInt64(&r.inProgressCnt, 1)
			go r.wrapRunnable(key)
			// Since the frontier contract has started running, we need
			// to remove it from r.readyContracts so that it won't
			// double-run.
			r.readyContracts.Delete(key)
			// The reason we use a separate readyCnt is because `sync.Map`
			// doesn't provide an atomic way to get its length.
			atomic.AddInt64(&r.readyCnt, -1)
			return true
		})
		// This corresponds to the "wait for any existing run (could be
		// from previous iteration) to finish" part in the pseudocode above.
		_, err := logging.LogIfNotDoneAfter(r.sdkCtx.Logger(), func() (struct{}, error) {
			<-r.someContractFinished
			return struct{}{}, nil
		}, LogAfter, "dex_parallel_runner_wait")
		if err != nil {
			// this should never happen
			panic(err)
		}
	}

	// make sure there is no orphaned goroutine blocked on channel send
	r.done <- struct{}{}
}

func (r *ParallelRunner) wrapRunnable(contractAddr types.ContractAddress) {
	defer func() {
		if err := recover(); err != nil {
			telemetry.IncrCounter(1, "recovered_panics")
			r.sdkCtx.Logger().Error(fmt.Sprintf("panic in parallel runner recovered: %s", err))
		}

		atomic.AddInt64(&r.inProgressCnt, -1) // this has to happen after any potential increment to readyCnt
		select {
		case r.someContractFinished <- struct{}{}:
		case <-r.done:
			// make sure other goroutines can also receive from 'done'
			r.done <- struct{}{}
		}
	}()

	contractInfo, _ := r.contractAddrToInfo.Load(contractAddr)
	r.runnable(*contractInfo)

	// Check if there is any contract that should be promoted to the frontier set.
	if contractInfo.Dependencies != nil {
		for _, dependency := range contractInfo.Dependencies {
			dependentContract := dependency.Dependency
			typedDependentContract := types.ContractAddress(dependentContract)
			dependentInfo, ok := r.contractAddrToInfo.Load(typedDependentContract)
			if !ok {
				// If we cannot find the dependency in the contract address info, then it's not a valid contract in this round
				r.sdkCtx.Logger().Error(fmt.Sprintf("Couldn't find dependency %s of contract %s in the contract address info", contractInfo.ContractAddr, dependentContract))
				continue
			}
			// It's okay to mutate ContractInfo here since it's a copy made in the runner's
			// constructor.
			newNumIncomingPaths := atomic.AddInt64(&dependentInfo.NumIncomingDependencies, -1)
			// This corresponds to the "for which all contracts that depend on it have
			// already finished." definition for frontier contract.
			if newNumIncomingPaths == 0 {
				r.readyContracts.Store(typedDependentContract, struct{}{})
				atomic.AddInt64(&r.readyCnt, 1)
			}
		}
	}
}
