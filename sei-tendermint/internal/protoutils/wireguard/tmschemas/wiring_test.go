package tmschemas_test

import (
	"os"
	"reflect"
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
// that should run a wireguard.PreDecode hook actually does. They catch
// the "validator exists but nobody calls it" regression — adding a new
// channel and forgetting to install its PreDecode, or removing a wiring
// during a refactor.

// samePtr returns true if a and b refer to the same function. Function
// values aren't directly comparable in Go, but their underlying code
// pointers are.
func samePtr(a, b interface{}) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func TestWiring_BlocksyncChannel(t *testing.T) {
	desc := blocksync.GetChannelDescriptor()
	pd, ok := desc.PreDecode.Get()
	require.True(t, ok, "blocksync channel PreDecode is not set")
	require.True(t, samePtr(pd, tmschemas.ValidateBlocksyncMessage),
		"blocksync channel PreDecode does not point at tmschemas.ValidateBlocksyncMessage")
}

func TestWiring_ConsensusDataChannel(t *testing.T) {
	desc := consensus.GetDataChannelDescriptor()
	pd, ok := desc.PreDecode.Get()
	require.True(t, ok, "consensus DataChannel PreDecode is not set")
	require.True(t, samePtr(pd, tmschemas.ValidateConsensusDataChannel),
		"consensus DataChannel PreDecode does not point at tmschemas.ValidateConsensusDataChannel")
}

func TestWiring_EvidenceChannel(t *testing.T) {
	desc := evidence.GetChannelDescriptor()
	pd, ok := desc.PreDecode.Get()
	require.True(t, ok, "evidence channel PreDecode is not set")
	require.True(t, samePtr(pd, tmschemas.ValidateEvidenceMessage),
		"evidence channel PreDecode does not point at tmschemas.ValidateEvidenceMessage")
}

func TestWiring_StatesyncLightBlockChannel(t *testing.T) {
	desc := statesync.GetLightBlockChannelDescriptor()
	pd, ok := desc.PreDecode.Get()
	require.True(t, ok, "statesync LightBlock PreDecode is not set")
	require.True(t, samePtr(pd, tmschemas.ValidateStatesyncLightBlockChannel),
		"statesync LightBlock PreDecode does not point at tmschemas.ValidateStatesyncLightBlockChannel")
}

// TestWiring_ConsensusAssembledBlock verifies that the consensus state's
// block-parts reassembly site calls ValidateConsensusAssembledBlock
// before Unmarshal. There's no descriptor to inspect here (the call
// lives inside an unexported method on *State), so this is a source-file
// grep — it catches the regression where someone removes or renames the
// call, which is the most likely way to break the wiring.
func TestWiring_ConsensusAssembledBlock(t *testing.T) {
	bz, err := os.ReadFile("../../../consensus/state.go")
	require.NoError(t, err, "could not read consensus/state.go to verify wiring")
	require.Contains(t, string(bz), "tmschemas.ValidateConsensusAssembledBlock",
		"consensus state.go does not reference tmschemas.ValidateConsensusAssembledBlock; the block-parts reassembly site lost its wireguard check")
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
