package contract_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

// A -> B -> C
func TestTopologicalSortContractInfoSimple(t *testing.T) {
	a := types.ContractInfoV2{
		ContractAddr: "A",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "B",
			},
		},
	}
	b := types.ContractInfoV2{
		ContractAddr: "B",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	c := types.ContractInfoV2{
		ContractAddr: "C",
	}
	res, err := contract.TopologicalSortContractInfo([]types.ContractInfoV2{b, c, a})
	require.Nil(t, err)
	require.Equal(t, "A", res[0].ContractAddr)
	require.Equal(t, "B", res[1].ContractAddr)
	require.Equal(t, "C", res[2].ContractAddr)
}

// A -> B, C -> D
func TestTopologicalSortContractInfoIsolated(t *testing.T) {
	a := types.ContractInfoV2{
		ContractAddr: "A",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "B",
			},
		},
	}
	b := types.ContractInfoV2{
		ContractAddr: "B",
	}
	c := types.ContractInfoV2{
		ContractAddr: "C",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "D",
			},
		},
	}
	d := types.ContractInfoV2{
		ContractAddr: "D",
	}
	res, err := contract.TopologicalSortContractInfo([]types.ContractInfoV2{b, c, a, d})
	require.Nil(t, err)
	aidx, bidx, cidx, didx := -1, -1, -1, -1
	for i, c := range res {
		if c.ContractAddr == "A" {
			aidx = i
		} else if c.ContractAddr == "B" {
			bidx = i
		} else if c.ContractAddr == "C" {
			cidx = i
		} else if c.ContractAddr == "D" {
			didx = i
		}
	}
	require.True(t, aidx != -1 && aidx < bidx)
	require.True(t, cidx != -1 && cidx < didx)
}

// A -> B -> C -> A
func TestTopologicalSortContractInfoCircular(t *testing.T) {
	a := types.ContractInfoV2{
		ContractAddr: "A",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "B",
			},
		},
	}
	b := types.ContractInfoV2{
		ContractAddr: "B",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "C",
			},
		},
	}
	c := types.ContractInfoV2{
		ContractAddr: "C",
		Dependencies: []*types.ContractDependencyInfo{
			{
				Dependency: "A",
			},
		},
	}
	res, err := contract.TopologicalSortContractInfo([]types.ContractInfoV2{b, c, a})
	require.NotNil(t, err)
	require.Equal(t, 0, len(res))
}
