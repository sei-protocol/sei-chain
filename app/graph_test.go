package app_test

import (
	"sort"
	"testing"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	"github.com/yourbasic/graph"
)

func TestCreateGraph(t *testing.T) {
	dag := app.NewDag()
	/**
	tx1: write to A, read B, commit 1
	tx2: read A, read B, commit 2
	tx3: read A, read B, commit 3
	tx4: write B, commit 4
	expected dag
	1wA -> 1rB -> 1c =>v 2rA -> 2rB ----=\---> 2c
	3rB -------------> 3rA -> 3c		  V
	\-----------------------------------=> 4wB -> 4c
	**/

	commitAccessOp := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_COMMIT,
		ResourceType:       acltypes.ResourceType_ANY,
		IdentifierTemplate: "*",
	}
	writeAccessA := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	readAccessA := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_READ,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	writeAccessB := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceB",
	}
	readAccessB := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_READ,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceB",
	}

	dag.AddNodeBuildDependency(0, 0, writeAccessA)   // node id 0
	dag.AddNodeBuildDependency(0, 0, readAccessB)    // node id 1
	dag.AddNodeBuildDependency(0, 0, commitAccessOp) // node id 2
	dag.AddNodeBuildDependency(0, 1, readAccessA)    // node id 3
	dag.AddNodeBuildDependency(0, 1, readAccessB)    // node id 4
	dag.AddNodeBuildDependency(0, 1, commitAccessOp) // node id 5
	dag.AddNodeBuildDependency(0, 2, readAccessB)    // node id 6
	dag.AddNodeBuildDependency(0, 2, readAccessA)    // node id 7
	dag.AddNodeBuildDependency(0, 2, commitAccessOp) // node id 8
	dag.AddNodeBuildDependency(0, 3, writeAccessB)   // node id 9
	dag.AddNodeBuildDependency(0, 3, commitAccessOp) // node id 10

	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[0])
	require.Equal(
		t,
		[]app.DagEdge{{1, 9}},
		dag.EdgesMap[1],
	)
	require.Equal(
		t,
		[]app.DagEdge{{2, 3}, {2, 7}},
		dag.EdgesMap[2],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[3])
	require.Equal(
		t,
		[]app.DagEdge{{4, 9}},
		dag.EdgesMap[4],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[5])
	require.Equal(
		t,
		[]app.DagEdge{{6, 9}},
		dag.EdgesMap[6],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[7])
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[8])
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[9])
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[10])

	// assert dag is acyclic
	acyclic := graph.Acyclic(dag)
	require.True(t, acyclic)

	// test completion signals
	completionSignalsMap, blockingSignalsMap := dag.BuildCompletionSignalMaps()

	channel0 := completionSignalsMap[0][0][commitAccessOp][0].Channel
	channel1 := completionSignalsMap[0][0][commitAccessOp][1].Channel
	channel2 := completionSignalsMap[1][0][readAccessB][0].Channel
	channel3 := completionSignalsMap[0][0][readAccessB][0].Channel
	channel4 := completionSignalsMap[2][0][readAccessB][0].Channel

	signal0 := app.CompletionSignal{2, 3, commitAccessOp, readAccessA, channel0}
	signal1 := app.CompletionSignal{2, 7, commitAccessOp, readAccessA, channel1}
	signal2 := app.CompletionSignal{4, 9, readAccessB, writeAccessB, channel2}
	signal3 := app.CompletionSignal{1, 9, readAccessB, writeAccessB, channel3}
	signal4 := app.CompletionSignal{6, 9, readAccessB, writeAccessB, channel4}

	require.Equal(
		t,
		[]app.CompletionSignal{signal0, signal1},
		completionSignalsMap[0][0][commitAccessOp],
	)
	require.Equal(
		t,
		[]app.CompletionSignal{signal0},
		blockingSignalsMap[1][0][readAccessA],
	)
	require.Equal(
		t,
		[]app.CompletionSignal{signal1},
		blockingSignalsMap[2][0][readAccessA],
	)

	require.Equal(
		t,
		[]app.CompletionSignal{signal2},
		completionSignalsMap[1][0][readAccessB],
	)
	require.Equal(
		t,
		[]app.CompletionSignal{signal3},
		completionSignalsMap[0][0][readAccessB],
	)
	require.Equal(
		t,
		[]app.CompletionSignal{signal4},
		completionSignalsMap[2][0][readAccessB],
	)
	slice := blockingSignalsMap[3][0][writeAccessB]
	sort.SliceStable(slice, func(p, q int) bool {
		return slice[p].FromNodeID < slice[q].FromNodeID
	})
	require.Equal(
		t,
		[]app.CompletionSignal{signal3, signal2, signal4},
		slice,
	)
}
