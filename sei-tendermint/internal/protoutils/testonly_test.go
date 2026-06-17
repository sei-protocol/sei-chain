package protoutils

import (
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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
		OneofValue: &pb.TestonlyMsg_OneofMessageValue{
			OneofMessageValue: &pb.TestonlyChild{Value: "o1"},
		},
		EnumValue:    pb.TestonlyEnum_TESTONLY_ENUM_SMALL,
		BoolValue:    true,
		Int32Value:   -1,
		Sint32Value:  -1,
		Uint32Value:  1,
		Fixed32Value: 2,
		Fixed64Value: 3,
		DoubleValue:  4,
		BytesValue:   []byte("bytes"),
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
		func(msg *pb.TestonlyMsg) { msg.OneofValue = nil },
		func(msg *pb.TestonlyMsg) { msg.GetOneofMessageValue().Value = "" },
		func(msg *pb.TestonlyMsg) { msg.EnumValue = pb.TestonlyEnum_TESTONLY_ENUM_UNSPECIFIED },
		func(msg *pb.TestonlyMsg) { msg.BoolValue = false },
		func(msg *pb.TestonlyMsg) { msg.Int32Value = 0 },
		func(msg *pb.TestonlyMsg) { msg.Sint32Value = 0 },
		func(msg *pb.TestonlyMsg) { msg.Uint32Value = 0 },
		func(msg *pb.TestonlyMsg) { msg.Fixed32Value = 0 },
		func(msg *pb.TestonlyMsg) { msg.Fixed64Value = 0 },
		func(msg *pb.TestonlyMsg) { msg.DoubleValue = 0 },
		func(msg *pb.TestonlyMsg) { msg.BytesValue = nil },
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
