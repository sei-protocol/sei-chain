package types

import (
	"sort"
	"testing"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/stretchr/testify/require"
	"github.com/yourbasic/graph"
)

func TestCreateGraph(t *testing.T) {
	dag := NewDag()
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

	commitAccessOp := *CommitAccessOp()
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

	require.Equal(t, []DagEdge(nil), dag.EdgesMap[0])
	require.Equal(
		t,
		[]DagEdge{{1, 9}},
		dag.EdgesMap[1],
	)
	require.Equal(
		t,
		[]DagEdge{{2, 3}, {2, 7}},
		dag.EdgesMap[2],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[3])
	require.Equal(
		t,
		[]DagEdge{{4, 9}},
		dag.EdgesMap[4],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[5])
	require.Equal(
		t,
		[]DagEdge{{6, 9}},
		dag.EdgesMap[6],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[7])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[8])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[9])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[10])

	// assert dag is acyclic
	acyclic := graph.Acyclic(dag)
	require.True(t, acyclic)

	// test completion signals
	completionSignalsMap, blockingSignalsMap := dag.CompletionSignalingMap, dag.BlockingSignalsMap

	channel0 := completionSignalsMap[0][0][commitAccessOp][0].Channel
	channel1 := completionSignalsMap[0][0][commitAccessOp][1].Channel
	channel2 := completionSignalsMap[1][0][readAccessB][0].Channel
	channel3 := completionSignalsMap[0][0][readAccessB][0].Channel
	channel4 := completionSignalsMap[2][0][readAccessB][0].Channel

	signal0 := CompletionSignal{2, 3, commitAccessOp, readAccessA, channel0}
	signal1 := CompletionSignal{2, 7, commitAccessOp, readAccessA, channel1}
	signal2 := CompletionSignal{4, 9, readAccessB, writeAccessB, channel2}
	signal3 := CompletionSignal{1, 9, readAccessB, writeAccessB, channel3}
	signal4 := CompletionSignal{6, 9, readAccessB, writeAccessB, channel4}

	require.Equal(
		t,
		[]CompletionSignal{signal0, signal1},
		completionSignalsMap[0][0][commitAccessOp],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal0},
		blockingSignalsMap[1][0][readAccessA],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal1},
		blockingSignalsMap[2][0][readAccessA],
	)

	require.Equal(
		t,
		[]CompletionSignal{signal2},
		completionSignalsMap[1][0][readAccessB],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal3},
		completionSignalsMap[0][0][readAccessB],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal4},
		completionSignalsMap[2][0][readAccessB],
	)
	slice := blockingSignalsMap[3][0][writeAccessB]
	sort.SliceStable(slice, func(p, q int) bool {
		return slice[p].FromNodeID < slice[q].FromNodeID
	})
	require.Equal(
		t,
		[]CompletionSignal{signal3, signal2, signal4},
		slice,
	)
}

func TestHierarchyDag(t *testing.T) {
	dag := NewDag()
	/**
	tx1: write to A, commit 1
	tx2: read ALL, commit 2
	tx3: write B mem, commit 3
	tx4: read A, commit 4
	expected dag
	1wA -> 1c => 2rALL -> 2c
			\	   \=> 3wB c3
			\---=> 4rA c4
	**/

	commit := *CommitAccessOp()
	writeA := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	readA := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_READ,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	writeB := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_Mem,
		IdentifierTemplate: "ResourceB",
	}
	readAll := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_READ,
		ResourceType:       acltypes.ResourceType_ANY,
		IdentifierTemplate: "*",
	}

	dag.AddNodeBuildDependency(0, 0, writeA)  // node id 0
	dag.AddNodeBuildDependency(0, 0, commit)  // node id 1
	dag.AddNodeBuildDependency(0, 1, readAll) // node id 2
	dag.AddNodeBuildDependency(0, 1, commit)  // node id 3
	dag.AddNodeBuildDependency(0, 2, writeB)  // node id 4
	dag.AddNodeBuildDependency(0, 2, commit)  // node id 5
	dag.AddNodeBuildDependency(0, 3, readA)   // node id 6
	dag.AddNodeBuildDependency(0, 3, commit)  // node id 7

	// assert dag is acyclic
	acyclic := graph.Acyclic(dag)
	require.True(t, acyclic)

	require.Equal(t, []DagEdge(nil), dag.EdgesMap[0])
	require.Equal(
		t,
		[]DagEdge{{1, 2}, {1, 6}},
		dag.EdgesMap[1],
	)
	require.Equal(
		t,
		[]DagEdge{{2, 4}},
		dag.EdgesMap[2],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[3])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[4])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[5])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[6])

	// test completion signals
	completionSignalsMap, blockingSignalsMap := dag.CompletionSignalingMap, dag.BlockingSignalsMap

	channel0 := completionSignalsMap[0][0][commit][0].Channel
	channel1 := completionSignalsMap[0][0][commit][1].Channel
	channel2 := completionSignalsMap[1][0][readAll][0].Channel

	signal0 := CompletionSignal{1, 2, commit, readAll, channel0}
	signal1 := CompletionSignal{1, 6, commit, readA, channel1}
	signal2 := CompletionSignal{2, 4, readAll, writeB, channel2}

	require.Equal(
		t,
		[]CompletionSignal{signal0, signal1},
		completionSignalsMap[0][0][commit],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal0},
		blockingSignalsMap[1][0][readAll],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal1},
		blockingSignalsMap[3][0][readA],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal2},
		completionSignalsMap[1][0][readAll],
	)
	require.Equal(
		t,
		[]CompletionSignal{signal2},
		blockingSignalsMap[2][0][writeB],
	)
}

func TestDagResourceIdentifiers(t *testing.T) {
	dag := NewDag()

	commit := *CommitAccessOp()
	writeA := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceA",
	}
	writeB := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceB",
	}
	writeStar := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "*",
	}
	writeC := acltypes.AccessOperation{
		AccessType:         acltypes.AccessType_WRITE,
		ResourceType:       acltypes.ResourceType_KV,
		IdentifierTemplate: "ResourceC",
	}

	dag.AddNodeBuildDependency(0, 0, writeA)    // node id 0
	dag.AddNodeBuildDependency(0, 0, commit)    // node id 1
	dag.AddNodeBuildDependency(0, 1, writeB)    // node id 2
	dag.AddNodeBuildDependency(0, 1, commit)    // node id 3
	dag.AddNodeBuildDependency(0, 2, writeStar) // node id 4
	dag.AddNodeBuildDependency(0, 2, commit)    // node id 5
	dag.AddNodeBuildDependency(0, 3, writeC)    // node id 6
	dag.AddNodeBuildDependency(0, 3, commit)    // node id 7

	acyclic := graph.Acyclic(dag)
	require.True(t, acyclic)
	// we expect there to be edges from 1 -> 4, 3 -> 4, and 5 -> 6
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[0])
	require.Equal(
		t,
		[]DagEdge{{1, 4}},
		dag.EdgesMap[1],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[2])
	require.Equal(
		t,
		[]DagEdge{{3, 4}},
		dag.EdgesMap[3],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[4])
	require.Equal(
		t,
		[]DagEdge{{5, 6}},
		dag.EdgesMap[5],
	)
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[6])
	require.Equal(t, []DagEdge(nil), dag.EdgesMap[7])
}
