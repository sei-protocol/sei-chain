package protoutils_test

import (
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"google.golang.org/protobuf/proto"
)

func TestIterMalformed(t *testing.T) {
	t.Parallel()

	msg := &pb.Msg{
		StringValue:   "root",
		RepeatedValue: []string{"a", "b"},
		MapValue: map[string]string{
			"x": "1",
			"y": "2",
		},
		RepeatedMessageValue: []*pb.Child{
			{Value: "m1"},
			{Value: "m2"},
		},
		MapMessageValue: map[string]*pb.Child{
			"p": {Value: "n1"},
			"q": {Value: "n2"},
		},
	}

	clearFields := utils.Slice(
		func(msg *pb.Msg) { msg.StringValue = "" },
		func(msg *pb.Msg) { msg.RepeatedValue = nil },
		func(msg *pb.Msg) { msg.MapValue = nil },
		func(msg *pb.Msg) { msg.RepeatedMessageValue = nil },
		func(msg *pb.Msg) { msg.RepeatedMessageValue[0].Value = "" },
		func(msg *pb.Msg) { msg.RepeatedMessageValue[1].Value = "" },
		func(msg *pb.Msg) { msg.MapMessageValue["p"].Value = "" },
		func(msg *pb.Msg) { msg.MapMessageValue["q"].Value = "" },
		func(msg *pb.Msg) { msg.MapMessageValue = nil },
	)

	var got []*pb.Msg
	conv := protoutils.Conv[*pb.Msg, *pb.Msg]{
		Encode: func(msg *pb.Msg) *pb.Msg { return protoutils.Clone(msg) },
		Decode: func(msg *pb.Msg) (*pb.Msg, error) {
			got = append(got, protoutils.Clone(msg))
			return protoutils.Clone(msg), nil
		},
	}

	require.NoError(t, conv.Test(msg))
	require.True(t, slices.ContainsFunc(got, func(gotMsg *pb.Msg) bool {
		return proto.Equal(gotMsg, msg)
	}), "missing original round-tripped message")

	for _, f := range clearFields {
		wantMsg := protoutils.Clone(msg)
		f(wantMsg)
		require.True(t, slices.ContainsFunc(got, func(gotMsg *pb.Msg) bool {
			return proto.Equal(gotMsg, wantMsg)
		}), "missing malformed variant: %v", wantMsg)
	}
}
