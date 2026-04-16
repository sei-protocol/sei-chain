package protoutils

import (
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"google.golang.org/protobuf/proto"
)

func TestIterMalformed(t *testing.T) {
	t.Parallel()

	msg := &pb.TestonlyMsg{
		StringValue:   "root",
		RepeatedValue: []string{"a", "b"},
		MapValue: map[string]string{
			"x": "1",
			"y": "2",
		},
		RepeatedMessageValue: []*pb.TestonlyChild{
			{Value: "m1"},
			{Value: "m2"},
		},
		MapMessageValue: map[string]*pb.TestonlyChild{
			"p": {Value: "n1"},
			"q": {Value: "n2"},
		},
	}

	clearFields := utils.Slice(
		func(msg *pb.TestonlyMsg) { msg.StringValue = "" },
		func(msg *pb.TestonlyMsg) { msg.RepeatedValue = nil },
		func(msg *pb.TestonlyMsg) { msg.MapValue = nil },
		func(msg *pb.TestonlyMsg) { msg.RepeatedMessageValue = nil },
		func(msg *pb.TestonlyMsg) { msg.RepeatedMessageValue[0].Value = "" },
		func(msg *pb.TestonlyMsg) { msg.RepeatedMessageValue[1].Value = "" },
		func(msg *pb.TestonlyMsg) { msg.MapMessageValue["p"].Value = "" },
		func(msg *pb.TestonlyMsg) { msg.MapMessageValue["q"].Value = "" },
		func(msg *pb.TestonlyMsg) { msg.MapMessageValue = nil },
	)

	got := slices.Collect(iterMalformed(msg))
	require.Len(t, got, len(clearFields))
	for _, f := range clearFields {
		wantMsg := Clone(msg)
		f(wantMsg)
		require.True(t, slices.ContainsFunc(got, func(gotMsg *pb.TestonlyMsg) bool {
			return proto.Equal(gotMsg, wantMsg)
		}), "missing malformed variant: %v", wantMsg)
	}
}
