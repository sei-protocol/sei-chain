package app_test

import (
	"testing"

	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
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

	dag.AddNodeBuildDependency(1, writeAccessA)   // node id 0
	dag.AddNodeBuildDependency(1, readAccessB)    // node id 1
	dag.AddNodeBuildDependency(1, commitAccessOp) // node id 2
	dag.AddNodeBuildDependency(2, readAccessA)    // node id 3
	dag.AddNodeBuildDependency(2, readAccessB)    // node id 4
	dag.AddNodeBuildDependency(2, commitAccessOp) // node id 5
	dag.AddNodeBuildDependency(3, readAccessB)    // node id 6
	dag.AddNodeBuildDependency(3, readAccessA)    // node id 7
	dag.AddNodeBuildDependency(3, commitAccessOp) // node id 8
	dag.AddNodeBuildDependency(4, writeAccessB)   // node id 9
	dag.AddNodeBuildDependency(4, commitAccessOp) // node id 10

	require.Equal(
		t,
		[]app.DagEdge{{0, 1}},
		dag.EdgesMap[0],
	)
	require.Equal(
		t,
		[]app.DagEdge{{1, 2}, {1, 9}},
		dag.EdgesMap[1],
	)
	require.Equal(
		t,
		[]app.DagEdge{{2, 3}, {2, 7}},
		dag.EdgesMap[2],
	)
	require.Equal(
		t,
		[]app.DagEdge{{3, 4}},
		dag.EdgesMap[3],
	)
	require.Equal(
		t,
		[]app.DagEdge{{4, 5}, {4, 9}},
		dag.EdgesMap[4],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[5])
	require.Equal(
		t,
		[]app.DagEdge{{6, 7}, {6, 9}},
		dag.EdgesMap[6],
	)
	require.Equal(
		t,
		[]app.DagEdge{{7, 8}},
		dag.EdgesMap[7],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[8])
	require.Equal(
		t,
		[]app.DagEdge{{9, 10}},
		dag.EdgesMap[9],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[10])

	// assert dag is acyclic
	acyclic := graph.Acyclic(dag)
	require.True(t, acyclic)
}
