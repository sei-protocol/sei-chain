package contract_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var (
	counter         int64 = 0
	dependencyCheck       = sync.Map{}
	testApp               = keepertest.TestApp()
	sdkCtx                = testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
)

func noopRunnable(_ types.ContractInfoV2) {
	atomic.AddInt64(&counter, 1)
}

func idleRunnable(_ types.ContractInfoV2) {
	time.Sleep(5 * time.Second)
	atomic.AddInt64(&counter, 1)
}

func panicRunnable(_ types.ContractInfoV2) {
	panic("")
}

func dependencyCheckRunnable(contractInfo types.ContractInfoV2) {
	if contractInfo.ContractAddr == "C" {
		_, hasA := dependencyCheck.Load("A")
		_, hasB := dependencyCheck.Load("B")
		if !hasA || !hasB {
			return
		}
	}
	dependencyCheck.Store(contractInfo.ContractAddr, struct{}{})
}

func TestRunnerSingleContract(t *testing.T) {
	counter = 0
	contractInfo := types.ContractInfoV2{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
	}
	runner := contract.NewParallelRunner(noopRunnable, []types.ContractInfoV2{contractInfo}, sdkCtx)
	runner.Run()
	require.Equal(t, int64(1), counter)
}

func TestRunnerParallelContract(t *testing.T) {
	counter = 0
	contractInfoA := types.ContractInfoV2{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
	}
	contractInfoB := types.ContractInfoV2{
		ContractAddr:            "B",
		NumIncomingDependencies: 0,
	}
	runner := contract.NewParallelRunner(idleRunnable, []types.ContractInfoV2{contractInfoA, contractInfoB}, sdkCtx)
	start := time.Now()
	runner.Run()
	end := time.Now()
	duration := end.Sub(start)
	require.Equal(t, int64(2), counter)
	require.True(t, duration.Seconds() < 10) // would not be flaky unless it's running on really slow hardware
}

func TestRunnerParallelContractWithDependency(t *testing.T) {
	counter = 0
	contractInfoA := types.ContractInfoV2{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	contractInfoB := types.ContractInfoV2{
		ContractAddr:            "B",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	contractInfoC := types.ContractInfoV2{
		ContractAddr:            "C",
		NumIncomingDependencies: 2,
	}
	runner := contract.NewParallelRunner(dependencyCheckRunnable, []types.ContractInfoV2{contractInfoC, contractInfoB, contractInfoA}, sdkCtx)
	runner.Run()
	_, hasC := dependencyCheck.Load("C")
	require.True(t, hasC)
}

func TestRunnerParallelContractWithInvalidDependency(t *testing.T) {
	dependencyCheck = sync.Map{}
	counter = 0
	contractInfoA := types.ContractInfoV2{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	contractInfoB := types.ContractInfoV2{
		ContractAddr:            "B",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	runner := contract.NewParallelRunner(dependencyCheckRunnable, []types.ContractInfoV2{contractInfoB, contractInfoA}, sdkCtx)
	runner.Run()
	_, hasC := dependencyCheck.Load("C")
	require.False(t, hasC)
}

func TestRunnerPanicContract(t *testing.T) {
	contractInfo := types.ContractInfoV2{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
	}
	runner := contract.NewParallelRunner(panicRunnable, []types.ContractInfoV2{contractInfo}, sdkCtx)
	require.NotPanics(t, runner.Run)
}
