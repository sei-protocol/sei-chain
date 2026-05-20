package statesync_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TestWiring_LightBlockChannel asserts that the statesync LightBlock
// channel descriptor installs a PreDecode hook that rejects an over-cap
// LightBlockResponse payload for SchemaForMessage.
func TestWiring_LightBlockChannel(t *testing.T) {
	pd, ok := statesync.GetLightBlockChannelDescriptor().PreDecode.Get()
	require.True(t, ok, "statesync LightBlock PreDecode is not set")
	msg := &ssproto.Message{Sum: &ssproto.Message_LightBlockResponse{
		LightBlockResponse: &ssproto.LightBlockResponse{
			LightBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: wgtest.CommitWith(wgtest.MaxCommitSignatures + 1)},
			},
		},
	}}
	require.Error(t, pd(wgtest.Marshal(t, msg)),
		"statesync LightBlock PreDecode failed to reject an over-cap Commit signatures list")
}

// TestWiring_OtherChannelsHaveNoPreDecode documents which statesync
// channels intentionally do NOT have a PreDecode hook. Only LightBlock
// reaches a Commit; the other three (Snapshot, Chunk, Params) carry
// payloads that have no signature-list cap to enforce.
func TestWiring_OtherChannelsHaveNoPreDecode(t *testing.T) {
	cases := []struct {
		name      string
		getHookFn func() bool
	}{
		{"Snapshot", func() bool { return statesync.GetSnapshotChannelDescriptor().PreDecode.IsPresent() }},
		{"Chunk", func() bool { return statesync.GetChunkChannelDescriptor().PreDecode.IsPresent() }},
		{"Params", func() bool { return statesync.GetParamsChannelDescriptor().PreDecode.IsPresent() }},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.name, ".", "_"), func(t *testing.T) {
			require.False(t, c.getHookFn(),
				"channel statesync.%s: expected no PreDecode hook, got one", c.name)
		})
	}
}
