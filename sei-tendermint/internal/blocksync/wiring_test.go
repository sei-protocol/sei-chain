package blocksync_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_BlocksyncChannel asserts that the blocksync channel descriptor
// installs a PreDecode hook that actually rejects an over-cap payload for
// SchemaForMessage. Missing wiring fails the Get() check; a wrong-schema
// hook accepts the payload tailored to the right schema.
func TestWiring_BlocksyncChannel(t *testing.T) {
	pd, ok := blocksync.GetChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "blocksync channel PreDecode is not set")
	msg := &bcproto.Message{Sum: &bcproto.Message_BlockResponse{
		BlockResponse: &bcproto.BlockResponse{
			Block: &tmproto.Block{LastCommit: wgtest.CommitWith(wgtest.MaxCommitSignatures + 1)},
		},
	}}
	require.Error(t, pd(wgtest.Marshal(t, msg)),
		"blocksync channel PreDecode failed to reject an over-cap last_commit")
}
