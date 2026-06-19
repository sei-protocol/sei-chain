package protoutils_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	testpb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
	crosspb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/b/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestScan_DescendsIntoNested(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.TestonlyOuter](protoutils.Marshal(&testpb.TestonlyOuter{
		Child: &testpb.TestonlyCountedLeaf{Items: []string{"a", "b", "c", "d"}},
	})))
}

func TestScan_CountsResetAcrossInstances(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlyOuter](protoutils.Marshal(&testpb.TestonlyOuter{
		Children: []*testpb.TestonlyCountedLeaf{
			{Items: []string{"a", "b"}},
			{Items: []string{"c", "d"}},
		},
	})))
}

func TestScan_MaxTotalSizeResetsAcrossNestedInstances(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlyOuter](protoutils.Marshal(&testpb.TestonlyOuter{
		SizedChildren: []*testpb.TestonlySizedLeaf{
			{Items: [][]byte{{1}, {2, 3}}},
			{Items: [][]byte{{4}, {5, 6}}},
		},
	})))
}

func TestScan_DuplicateNonRepeatedMessagesGetSeparateChildBudgets(t *testing.T) {
	child := protoutils.Marshal(&testpb.TestonlyCountedLeaf{Items: []string{"a", "b"}})
	var raw []byte
	for range 2 {
		raw = protowire.AppendTag(raw, 1, protowire.BytesType)
		raw = protowire.AppendVarint(raw, uint64(len(child)))
		raw = append(raw, child...)
	}
	require.NoError(t, protoutils.Scan[*testpb.TestonlyOuter](raw))
}

func TestScan_DistinctSchemasStayIndependent(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlyDistinct](protoutils.Marshal(&testpb.TestonlyDistinct{
		A: &testpb.TestonlyLeafA{Items: []string{"a", "b"}},
		B: &testpb.TestonlyLeafB{Items: []string{"c", "d"}},
	})))
}

func TestScan_NestedWithExplicitMaxCount(t *testing.T) {
	msg := &testpb.TestonlyOuter{}
	for range 5 {
		msg.Children = append(msg.Children, &testpb.TestonlyCountedLeaf{})
	}
	require.NoError(t, protoutils.Scan[*testpb.TestonlyOuter](protoutils.Marshal(msg)))
	msg.Children = append(msg.Children, &testpb.TestonlyCountedLeaf{})
	require.Error(t, protoutils.Scan[*testpb.TestonlyOuter](protoutils.Marshal(msg)))
}

func TestScan_DeepNestingBoundedCorrectly(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.TestonlyRoot](protoutils.Marshal(&testpb.TestonlyRoot{
		Mid: &testpb.TestonlyMid{
			Child: &testpb.TestonlyCountedLeaf{Items: []string{"a", "b", "c", "d"}},
		},
	})))
}

func TestScan_UnannotatedTargetIsNoOp(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlyUnannotatedParent](protoutils.Marshal(&testpb.TestonlyUnannotatedParent{
		Child: &testpb.TestonlyUnannotatedChild{
			Items:   []string{"a", "b", "c", "d", "e", "f"},
			Payload: []byte("payload that would be rejected if capped"),
		},
	})))
}

func TestScan_DescendsIntoOneofVariant(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.TestonlyOneofOuter](protoutils.Marshal(&testpb.TestonlyOneofOuter{
		Choice: &testpb.TestonlyOneofOuter_Inner{
			Inner: &testpb.TestonlyOneofInner{Items: []string{"a", "b", "c", "d"}},
		},
	})))
}

func TestScan_EnforcesSizeRules(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.TestonlySizedParent](protoutils.Marshal(&testpb.TestonlySizedParent{
		Payload: [][]byte{{1, 2, 3, 4}},
	})))

	require.Error(t, protoutils.Scan[*testpb.TestonlySizedParent](protoutils.Marshal(&testpb.TestonlySizedParent{
		Payload: [][]byte{{1, 2, 3}, {4, 5, 6}},
	})))

	require.Error(t, protoutils.Scan[*testpb.TestonlySizedParent](protoutils.Marshal(&testpb.TestonlySizedParent{
		Child: &testpb.TestonlySizedName{Name: "abcde"},
	})))

	require.NoError(t, protoutils.Scan[*testpb.TestonlySizedParent](protoutils.Marshal(&testpb.TestonlySizedParent{
		Payload: [][]byte{{1, 2}, {3, 4, 5}},
		Child:   &testpb.TestonlySizedName{Name: "abcd"},
	})))
}

func TestScan_DescendsAcrossPackages(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.TestonlyCrossParent](protoutils.Marshal(&testpb.TestonlyCrossParent{
		Child: &crosspb.TestonlyCrossLeaf{
			Child: &crosspb.TestonlyCrossInner{Items: []string{"a", "b", "c", "d"}},
		},
	})))
}

func TestScan_AcceptsSizedNestedMessage(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlySizedMessageOuter](protoutils.Marshal(&testpb.TestonlySizedMessageOuter{
		Inner: &testpb.TestonlySizedMessageInner{Payload: []byte("12345678")},
	})))

	require.Error(t, protoutils.Scan[*testpb.TestonlySizedMessageOuter](protoutils.Marshal(&testpb.TestonlySizedMessageOuter{
		Inner: &testpb.TestonlySizedMessageInner{Payload: []byte("123456789")},
	})))
}

func TestScan_EnforcesRepeatedNestedTotalSize(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlyRepeatedSizedChildren](protoutils.Marshal(&testpb.TestonlyRepeatedSizedChildren{
		Children: []*testpb.TestonlySizedName{
			{Name: "abcd"},
			{Name: "wxyz"},
		},
	})))

	require.Error(t, protoutils.Scan[*testpb.TestonlyRepeatedSizedChildren](protoutils.Marshal(&testpb.TestonlyRepeatedSizedChildren{
		Children: []*testpb.TestonlySizedName{
			{Name: "abcd"},
			{Name: "wxyz"},
			{Name: "zzzz"},
		},
	})))
}

func TestScan_Proto3OptionalDoesNotInterfere(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.TestonlyOptional](protoutils.Marshal(&testpb.TestonlyOptional{
		Bar:   proto.String("present"),
		Items: []string{"a", "b", "c", "d", "e"},
	})))

	require.Error(t, protoutils.Scan[*testpb.TestonlyOptional](protoutils.Marshal(&testpb.TestonlyOptional{
		Bar:   proto.String("present"),
		Items: []string{"a", "b", "c", "d", "e", "f"},
	})))
}
