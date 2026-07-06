package consensus_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_DataChannel asserts that the consensus message type is
// registered with wireguard and rejects an over-cap Proposal payload.
func TestWiring_DataChannel(t *testing.T) {
	msg := &tmcons.Message{Sum: &tmcons.Message_Proposal{
		Proposal: &tmcons.Proposal{Proposal: tmproto.Proposal{
			LastCommit: commitWith(maxCommitSignatures + 1),
		}},
	}}
	require.Error(t, protoutils.Scan[*tmcons.Message](marshal(t, msg)),
		"protoutils.Scan[*consensus.Message] failed to reject an over-cap last_commit")
}

// TestWiring_OtherChannelsAreNoOp documents that State, Vote, and VoteSet
// messages don't reach a Commit path, so protoutils.Scan is a no-op for them.
func TestWiring_OtherChannelsAreNoOp(t *testing.T) {
	cases := []struct {
		name string
		msg  *tmcons.Message
	}{
		{"State", &tmcons.Message{Sum: &tmcons.Message_NewRoundStep{NewRoundStep: &tmcons.NewRoundStep{}}}},
		{"Vote", &tmcons.Message{Sum: &tmcons.Message_Vote{Vote: &tmcons.Vote{}}}},
		{"VoteSet", &tmcons.Message{Sum: &tmcons.Message_VoteSetBits{VoteSetBits: &tmcons.VoteSetBits{}}}},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.name, ".", "_"), func(t *testing.T) {
			require.NoError(t, protoutils.Scan[*tmcons.Message](marshal(t, c.msg)),
				"consensus %s message should be a no-op for protoutils.Scan", c.name)
		})
	}
}

// TestWiring_AssembledBlock verifies that the consensus state's block-parts
// reassembly site calls protoutils.Scan[*tmproto.Block] before Unmarshal. The call
// lives inside an unexported method on *State that needs a fully-set-up
// state to exercise, so this is a source-file grep — it catches the
// regression where someone removes or renames the call, which is the
// likeliest way to break the wiring. Block scan's own rejection
// contract is covered by TestSchemaForBlock_* in proto/tendermint/types.
func TestWiring_AssembledBlock(t *testing.T) {
	bz, err := os.ReadFile("state.go")
	require.NoError(t, err, "could not read consensus/state.go to verify wiring")
	require.Contains(t, string(bz), "protoutils.Scan[*tmproto.Block]",
		"consensus state.go does not reference protoutils.Scan[*tmproto.Block]; the block-parts reassembly site lost its wireguard check")
}
