package statesync_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_LightBlockChannel asserts that the statesync message type
// is registered with wireguard and rejects an over-cap LightBlockResponse payload.
func TestWiring_LightBlockChannel(t *testing.T) {
	msg := &ssproto.Message{Sum: &ssproto.Message_LightBlockResponse{
		LightBlockResponse: &ssproto.LightBlockResponse{
			LightBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: commitWith(maxCommitSignatures + 1)},
			},
		},
	}}
	require.Error(t, protoutils.Scan[*ssproto.Message](marshal(t, msg)),
		"protoutils.Scan[*statesync.Message] failed to reject an over-cap Commit signatures list")
}

// TestWiring_OtherChannelsAreNoOp documents that Snapshot, Chunk, and Params
// messages don't reach a Commit path, so protoutils.Scan is a no-op for them.
func TestWiring_OtherChannelsAreNoOp(t *testing.T) {
	cases := []struct {
		name string
		msg  *ssproto.Message
	}{
		{"Snapshot", &ssproto.Message{Sum: &ssproto.Message_SnapshotsResponse{SnapshotsResponse: &ssproto.SnapshotsResponse{}}}},
		{"Chunk", &ssproto.Message{Sum: &ssproto.Message_ChunkResponse{ChunkResponse: &ssproto.ChunkResponse{}}}},
		{"Params", &ssproto.Message{Sum: &ssproto.Message_ParamsResponse{ParamsResponse: &ssproto.ParamsResponse{}}}},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.name, ".", "_"), func(t *testing.T) {
			require.NoError(t, protoutils.Scan[*ssproto.Message](marshal(t, c.msg)),
				"statesync %s message should be a no-op for protoutils.Scan", c.name)
		})
	}
}
