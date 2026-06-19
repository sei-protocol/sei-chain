package blocksync_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_BlocksyncChannel asserts that the blocksync message type
// is registered with wireguard and rejects an over-cap last_commit payload.
func TestWiring_BlocksyncChannel(t *testing.T) {
	msg := &bcproto.Message{Sum: &bcproto.Message_BlockResponse{
		BlockResponse: &bcproto.BlockResponse{
			Block: &tmproto.Block{LastCommit: commitWith(maxCommitSignatures + 1)},
		},
	}}
	require.Error(t, protoutils.Scan[*bcproto.Message](marshal(t, msg)),
		"protoutils.Scan[*blocksync.Message] failed to reject an over-cap last_commit")
}
