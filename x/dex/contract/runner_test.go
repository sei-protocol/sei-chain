package contract_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

var (
	counter         int64 = 0
	dependencyCheck       = sync.Map{}
)

func noopRunnable(_ types.ContractInfo) {
	atomic.AddInt64(&counter, 1)
}

func idleRunnable(_ types.ContractInfo) {
	time.Sleep(5 * time.Second)
	atomic.AddInt64(&counter, 1)
}

func dependencyCheckRunnable(contractInfo types.ContractInfo) {
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
	contractInfo := types.ContractInfo{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
	}
	runner := contract.NewParallelRunner(noopRunnable, []types.ContractInfo{contractInfo})
	runner.Run()
	require.Equal(t, int64(1), counter)
}

func TestRunnerParallelContract(t *testing.T) {
	counter = 0
	contractInfoA := types.ContractInfo{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
	}
	contractInfoB := types.ContractInfo{
		ContractAddr:            "B",
		NumIncomingDependencies: 0,
	}
	runner := contract.NewParallelRunner(idleRunnable, []types.ContractInfo{contractInfoA, contractInfoB})
	start := time.Now()
	runner.Run()
	end := time.Now()
	duration := end.Sub(start)
	require.Equal(t, int64(2), counter)
	require.True(t, duration.Seconds() < 10) // would not be flaky unless it's running on really slow hardware
}

func TestRunnerParallelContractWithDependency(t *testing.T) {
	counter = 0
	contractInfoA := types.ContractInfo{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	contractInfoB := types.ContractInfo{
		ContractAddr:            "B",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	contractInfoC := types.ContractInfo{
		ContractAddr:            "C",
		NumIncomingDependencies: 2,
	}
	runner := contract.NewParallelRunner(dependencyCheckRunnable, []types.ContractInfo{contractInfoC, contractInfoB, contractInfoA})
	runner.Run()
	_, hasC := dependencyCheck.Load("C")
	require.True(t, hasC)
}

func TestRunnerParallelContractWithInvalidDependency(t *testing.T) {
	dependencyCheck = sync.Map{}
	counter = 0
	contractInfoA := types.ContractInfo{
		ContractAddr:            "A",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	contractInfoB := types.ContractInfo{
		ContractAddr:            "B",
		NumIncomingDependencies: 0,
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	runner := contract.NewParallelRunner(dependencyCheckRunnable, []types.ContractInfo{contractInfoB, contractInfoA})
	runner.Run()
	_, hasC := dependencyCheck.Load("C")
	require.False(t, hasC)
}
