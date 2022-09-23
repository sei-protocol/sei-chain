package app_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	"github.com/yourbasic/graph"
)

func TestCreateGraph(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	wrapper := app.NewTestWrapper(t, tm, valPub)
	dag := app.NewDag()
	/**
	tx1: write to A, read B
	tx2: read A, read B
	tx3: read A, read B
	tx4: write B
	expected dag
	1wA -> 1rB =>v 2rA -> 2rB =\
	3rB ------> 3rA			   V
	  \--------------------=> 4wB
	**/

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

	dag.AddNodeBuildDependency(wrapper.Ctx, 1, writeAccessA) // node id 0
	dag.AddNodeBuildDependency(wrapper.Ctx, 1, readAccessB)  // node id 1
	dag.AddNodeBuildDependency(wrapper.Ctx, 2, readAccessA)  // node id 2
	dag.AddNodeBuildDependency(wrapper.Ctx, 2, readAccessB)  // node id 3
	dag.AddNodeBuildDependency(wrapper.Ctx, 3, readAccessB)  // node id 4
	dag.AddNodeBuildDependency(wrapper.Ctx, 3, readAccessA)  // node id 5
	dag.AddNodeBuildDependency(wrapper.Ctx, 4, writeAccessB) // node id 6

	require.Equal(
		t,
		[]app.DagEdge{{0, 1, nil}},
		dag.EdgesMap[0],
	)
	require.Equal(
		t,
		[]app.DagEdge{{1, 2, &writeAccessA}, {1, 5, &writeAccessA}, {1, 6, &readAccessB}},
		dag.EdgesMap[1],
	)
	require.Equal(
		t,
		[]app.DagEdge{{2, 3, nil}},
		dag.EdgesMap[2],
	)
	require.Equal(
		t,
		[]app.DagEdge{{3, 6, &readAccessB}},
		dag.EdgesMap[3],
	)
	require.Equal(
		t,
		[]app.DagEdge{{4, 5, nil}, {4, 6, &readAccessB}},
		dag.EdgesMap[4],
	)
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[5])
	require.Equal(t, []app.DagEdge(nil), dag.EdgesMap[6])

	// assert dag is acyclic
	acyclic := graph.Acyclic(dag)
	require.True(t, acyclic)
}
