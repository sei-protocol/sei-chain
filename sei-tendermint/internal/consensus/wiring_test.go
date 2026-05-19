package consensus_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_DataChannel asserts that DataChannel has a PreDecode hook
// installed that rejects an over-cap Proposal payload for SchemaForMessage.
func TestWiring_DataChannel(t *testing.T) {
	pd, ok := consensus.GetDataChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "consensus DataChannel PreDecode is not set")
	msg := &tmcons.Message{Sum: &tmcons.Message_Proposal{
		Proposal: &tmcons.Proposal{Proposal: tmproto.Proposal{
			LastCommit: wgtest.CommitWith(wgtest.MaxCommitSignatures + 1),
		}},
	}}
	require.Error(t, pd(wgtest.Marshal(t, msg)),
		"consensus DataChannel PreDecode failed to reject an over-cap last_commit")
}

// TestWiring_AssembledBlock verifies that the consensus state's block-parts
// reassembly site calls SchemaForBlock.Scan before Unmarshal. The call
// lives inside an unexported method on *State that needs a fully-set-up
// state to exercise, so this is a source-file grep — it catches the
// regression where someone removes or renames the call, which is the
// likeliest way to break the wiring. SchemaForBlock's own rejection
// contract is covered by TestSchemaForBlock_* in proto/tendermint/types.
func TestWiring_AssembledBlock(t *testing.T) {
	bz, err := os.ReadFile("state.go")
	require.NoError(t, err, "could not read consensus/state.go to verify wiring")
	require.Contains(t, string(bz), "tmproto.SchemaForBlock.Scan",
		"consensus state.go does not reference tmproto.SchemaForBlock.Scan; the block-parts reassembly site lost its wireguard check")
}

// TestWiring_OtherChannelsHaveNoPreDecode documents which consensus
// channels intentionally do NOT have a PreDecode hook. Only DataChannel
// carries Proposal; the rest carry votes / vote-sets / round-step
// messages that don't reach a Commit.
func TestWiring_OtherChannelsHaveNoPreDecode(t *testing.T) {
	cases := []struct {
		name      string
		getHookFn func() bool
	}{
		{"State", func() bool { return consensus.GetStateChannelDescriptor().PreDecode.IsPresent() }},
		{"Vote", func() bool { return consensus.GetVoteChannelDescriptor().PreDecode.IsPresent() }},
		{"VoteSet", func() bool { return consensus.GetVoteSetChannelDescriptor().PreDecode.IsPresent() }},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.name, ".", "_"), func(t *testing.T) {
			require.False(t, c.getHookFn(),
				"channel consensus.%s: expected no PreDecode hook, got one", c.name)
		})
	}
}
