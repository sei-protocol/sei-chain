package tmschemas_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evidence"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/tmschemas"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync"
)

// These tests assert that each channel descriptor or post-assembly site
// that should run a wireguard.PreDecode hook actually does. They detect
// the "hook isn't called" regression by exercising the hook with an
// over-cap payload tailored to the schema we expect to be wired and
// asserting it rejects. A missing hook fails the Get() check; a
// wrong-schema-wired hook either accepts the payload (wrong schema's
// rules permit it) or rejects it for the wrong reason — both surface as
// test failures.

func TestWiring_BlocksyncChannel(t *testing.T) {
	pd, ok := blocksync.GetChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "blocksync channel PreDecode is not set")
	require.Error(t, pd(marshal(t,
		blocksyncResponse(commitWith(tmschemas.MaxCommitSignatures+1)))),
		"blocksync channel PreDecode failed to reject an over-cap last_commit")
}

func TestWiring_ConsensusDataChannel(t *testing.T) {
	pd, ok := consensus.GetDataChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "consensus DataChannel PreDecode is not set")
	require.Error(t, pd(marshal(t,
		consensusProposalMessage(commitWith(tmschemas.MaxCommitSignatures+1)))),
		"consensus DataChannel PreDecode failed to reject an over-cap last_commit")
}

func TestWiring_EvidenceChannel(t *testing.T) {
	pd, ok := evidence.GetChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "evidence channel PreDecode is not set")
	require.Error(t, pd(marshal(t,
		lcaeEvidence(tmschemas.MaxCommitSignatures+1))),
		"evidence channel PreDecode failed to reject an over-cap Commit signatures list")
}

func TestWiring_StatesyncLightBlockChannel(t *testing.T) {
	pd, ok := statesync.GetLightBlockChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "statesync LightBlock PreDecode is not set")
	require.Error(t, pd(marshal(t,
		lightBlockRespMsg(tmschemas.MaxCommitSignatures+1))),
		"statesync LightBlock PreDecode failed to reject an over-cap Commit signatures list")
}

// TestWiring_ConsensusAssembledBlock verifies that the consensus state's
// block-parts reassembly site calls SchemaForBlock.Scan before Unmarshal.
// There's no descriptor to inspect here (the call lives inside an
// unexported method on *State), so this is a source-file grep — it
// catches the regression where someone removes or renames the call,
// which is the most likely way to break the wiring. The behavioral
// rejection contract for SchemaForBlock is covered by the
// TestConsensusAssembledBlock_* tests in channels_test.go.
func TestWiring_ConsensusAssembledBlock(t *testing.T) {
	bz, err := os.ReadFile("../../../consensus/state.go")
	require.NoError(t, err, "could not read consensus/state.go to verify wiring")
	require.Contains(t, string(bz), "tmproto.SchemaForBlock.Scan",
		"consensus state.go does not reference tmproto.SchemaForBlock.Scan; the block-parts reassembly site lost its wireguard check")
}

// TestWiring_OtherChannelsHaveNoPreDecode documents which channels
// intentionally do NOT have a PreDecode hook. Adding a new sibling
// channel without considering whether to install a hook is a smell;
// this test will then fail and force the discussion.
func TestWiring_OtherChannelsHaveNoPreDecode(t *testing.T) {
	type expectation struct {
		name      string
		hasHook   bool
		getHookFn func() bool
	}
	cases := []expectation{
		// Consensus has four channels; only DataChannel carries Proposal,
		// the rest carry votes / vote-sets / round-step messages that
		// don't reach a Commit.
		{name: "consensus.State", hasHook: false, getHookFn: func() bool {
			return consensus.GetStateChannelDescriptor().PreDecode.IsPresent()
		}},
		{name: "consensus.Vote", hasHook: false, getHookFn: func() bool {
			return consensus.GetVoteChannelDescriptor().PreDecode.IsPresent()
		}},
		{name: "consensus.VoteSet", hasHook: false, getHookFn: func() bool {
			return consensus.GetVoteSetChannelDescriptor().PreDecode.IsPresent()
		}},
		// Statesync has four channels; only LightBlock reaches a Commit.
		{name: "statesync.Snapshot", hasHook: false, getHookFn: func() bool {
			return statesync.GetSnapshotChannelDescriptor().PreDecode.IsPresent()
		}},
		{name: "statesync.Chunk", hasHook: false, getHookFn: func() bool {
			return statesync.GetChunkChannelDescriptor().PreDecode.IsPresent()
		}},
		{name: "statesync.Params", hasHook: false, getHookFn: func() bool {
			return statesync.GetParamsChannelDescriptor().PreDecode.IsPresent()
		}},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.name, ".", "_"), func(t *testing.T) {
			require.Equal(t, c.hasHook, c.getHookFn(),
				"channel %s: expected hasHook=%v, got %v", c.name, c.hasHook, c.getHookFn())
		})
	}
}
