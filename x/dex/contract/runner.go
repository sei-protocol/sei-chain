package contract

import (
	"sync"
	"sync/atomic"

	seiutils "github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

type ParallelRunner struct {
	runnable func(contract types.ContractInfo)

	contractAddrToInfo   *sync.Map // of type map[utils.ContractAddress]*types.ContractInfo
	readyContracts       *sync.Map // of type map[utils.ContractAddress]struct{}
	readyCnt             int64
	inProgressCnt        int64
	someContractFinished chan struct{}
}

func NewParallelRunner(runnable func(contract types.ContractInfo), contracts []types.ContractInfo) ParallelRunner {
	contractAddrToInfo := sync.Map{}
	contractsFrontier := sync.Map{}
	for _, contract := range contracts {
		// runner will mutate ContractInfo fields
		copy := contract
		typedContractAddr := utils.ContractAddress(contract.ContractAddr)
		contractAddrToInfo.Store(typedContractAddr, &copy)
		if copy.NumIncomingPaths == 0 {
			contractsFrontier.Store(typedContractAddr, struct{}{})
		}
	}
	return ParallelRunner{
		runnable:             runnable,
		contractAddrToInfo:   &contractAddrToInfo,
		readyContracts:       &contractsFrontier,
		readyCnt:             int64(seiutils.SyncMapLen(&contractsFrontier)),
		inProgressCnt:        0,
		someContractFinished: make(chan struct{}),
	}
}

func (r *ParallelRunner) Run() {
	// The ordering of the two conditions below matters
	for r.inProgressCnt > 0 || r.readyCnt > 0 {
		r.readyContracts.Range(func(key, _ any) bool {
			atomic.AddInt64(&r.inProgressCnt, 1)
			go r.wrapRunnable(key.(utils.ContractAddress))
			r.readyContracts.Delete(key)
			atomic.AddInt64(&r.readyCnt, -1)
			return true
		})
		// We still have correctness guarantee even without this channel.
		// It's added only to prevent unnecessary loops.
		<-r.someContractFinished
	}
}

func (r *ParallelRunner) wrapRunnable(contractAddr utils.ContractAddress) {
	contractInfo, _ := r.contractAddrToInfo.Load(contractAddr)
	typedContractInfo := contractInfo.(*types.ContractInfo)
	r.runnable(*typedContractInfo)

	if typedContractInfo.DependentContractAddrs != nil {
		for _, dependentContract := range typedContractInfo.DependentContractAddrs {
			typedDependentContract := utils.ContractAddress(dependentContract)
			dependentInfo, _ := r.contractAddrToInfo.Load(typedDependentContract)
			typedDependentInfo := dependentInfo.(*types.ContractInfo)
			newNumIncomingPaths := atomic.AddInt64(&typedDependentInfo.NumIncomingPaths, -1)
			if newNumIncomingPaths == 0 {
				r.readyContracts.Store(typedDependentContract, struct{}{})
				atomic.AddInt64(&r.readyCnt, 1)
			}
		}
	}

	atomic.AddInt64(&r.inProgressCnt, -1) // this has to happen after any potential increment to readyCnt
	r.someContractFinished <- struct{}{}
}
