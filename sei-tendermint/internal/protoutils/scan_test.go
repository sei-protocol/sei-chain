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

func TestScan_NestedRepeated(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.Outer](protoutils.Marshal(&testpb.Outer{
		// Nested message with field exceeding max_count is BAD.
		Child: &testpb.CountedLeaf{Items: []string{"a", "b", "c", "d"}},
	})))
	require.NoError(t, protoutils.Scan[*testpb.Outer](protoutils.Marshal(&testpb.Outer{
		// Multiple nested messages, each OK, then total is OK
		Children: []*testpb.CountedLeaf{
			{Items: []string{"a", "b"}},
			{Items: []string{"c", "d"}},
		},
	})))
}

func TestScan_MaxTotalSizeResetsAcrossNestedInstances(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.Outer](protoutils.Marshal(&testpb.Outer{
		SizedChildren: []*testpb.SizedLeaf{
			{Items: [][]byte{{1}, {2, 3}}},
			{Items: [][]byte{{4}, {5, 6}}},
		},
	})))
}

func TestScan_DuplicateNonRepeatedMessagesGetSeparateChildBudgets(t *testing.T) {
	child := protoutils.Marshal(&testpb.CountedLeaf{Items: []string{"a", "b"}})
	var raw []byte
	for range 2 {
		raw = protowire.AppendTag(raw, 1, protowire.BytesType)
		raw = protowire.AppendVarint(raw, uint64(len(child)))
		raw = append(raw, child...)
	}
	require.NoError(t, protoutils.Scan[*testpb.Outer](raw))
}

func TestScan_DistinctSchemasStayIndependent(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.Distinct](protoutils.Marshal(&testpb.Distinct{
		A: &testpb.LeafA{Items: []string{"a", "b"}},
		B: &testpb.LeafB{Items: []string{"c", "d"}},
	})))
}

func TestScan_NestedWithExplicitMaxCount(t *testing.T) {
	msg := &testpb.Outer{}
	for range 5 {
		msg.Children = append(msg.Children, &testpb.CountedLeaf{})
	}
	require.NoError(t, protoutils.Scan[*testpb.Outer](protoutils.Marshal(msg)))
	msg.Children = append(msg.Children, &testpb.CountedLeaf{})
	require.Error(t, protoutils.Scan[*testpb.Outer](protoutils.Marshal(msg)))
}

func TestScan_DeepNestingBoundedCorrectly(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.Root](protoutils.Marshal(&testpb.Root{
		Mid: &testpb.Mid{
			Child: &testpb.CountedLeaf{Items: []string{"a", "b", "c", "d"}},
		},
	})))
}

func TestScan_UnannotatedTargetIsNoOp(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.UnannotatedParent](protoutils.Marshal(&testpb.UnannotatedParent{
		Child: &testpb.UnannotatedChild{
			Items:   []string{"a", "b", "c", "d", "e", "f"},
			Payload: []byte("payload that would be rejected if capped"),
		},
	})))
}

func TestScan_DescendsIntoOneofVariant(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.OneofOuter](protoutils.Marshal(&testpb.OneofOuter{
		Choice: &testpb.OneofOuter_Inner{
			Inner: &testpb.OneofInner{Items: []string{"a", "b", "c", "d"}},
		},
	})))
}

func TestScan_EnforcesSizeRules(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.SizedParent](protoutils.Marshal(&testpb.SizedParent{
		Payload: [][]byte{{1, 2, 3, 4}},
	})))

	require.Error(t, protoutils.Scan[*testpb.SizedParent](protoutils.Marshal(&testpb.SizedParent{
		Payload: [][]byte{{1, 2, 3}, {4, 5, 6}},
	})))

	require.Error(t, protoutils.Scan[*testpb.SizedParent](protoutils.Marshal(&testpb.SizedParent{
		Child: &testpb.SizedName{Name: "abcde"},
	})))

	require.NoError(t, protoutils.Scan[*testpb.SizedParent](protoutils.Marshal(&testpb.SizedParent{
		Payload: [][]byte{{1, 2}, {3, 4, 5}},
		Child:   &testpb.SizedName{Name: "abcd"},
	})))
}

func TestScan_DescendsAcrossPackages(t *testing.T) {
	require.Error(t, protoutils.Scan[*testpb.CrossParent](protoutils.Marshal(&testpb.CrossParent{
		Child: &crosspb.CrossLeaf{
			Child: &crosspb.CrossInner{Items: []string{"a", "b", "c", "d"}},
		},
	})))
}

func TestScan_AcceptsSizedNestedMessage(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.SizedMessageOuter](protoutils.Marshal(&testpb.SizedMessageOuter{
		Inner: &testpb.SizedMessageInner{Payload: []byte("12345678")},
	})))

	require.Error(t, protoutils.Scan[*testpb.SizedMessageOuter](protoutils.Marshal(&testpb.SizedMessageOuter{
		Inner: &testpb.SizedMessageInner{Payload: []byte("123456789")},
	})))
}

func TestScan_EnforcesRepeatedNestedTotalSize(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.RepeatedSizedChildren](protoutils.Marshal(&testpb.RepeatedSizedChildren{
		Children: []*testpb.SizedName{
			{Name: "abcd"},
			{Name: "wxyz"},
		},
	})))

	require.Error(t, protoutils.Scan[*testpb.RepeatedSizedChildren](protoutils.Marshal(&testpb.RepeatedSizedChildren{
		Children: []*testpb.SizedName{
			{Name: "abcd"},
			{Name: "wxyz"},
			{Name: "zzzz"},
		},
	})))
}

func TestScan_Proto3OptionalDoesNotInterfere(t *testing.T) {
	require.NoError(t, protoutils.Scan[*testpb.Optional](protoutils.Marshal(&testpb.Optional{
		Bar:   proto.String("present"),
		Items: []string{"a", "b", "c", "d", "e"},
	})))

	require.Error(t, protoutils.Scan[*testpb.Optional](protoutils.Marshal(&testpb.Optional{
		Bar:   proto.String("present"),
		Items: []string{"a", "b", "c", "d", "e", "f"},
	})))
}
