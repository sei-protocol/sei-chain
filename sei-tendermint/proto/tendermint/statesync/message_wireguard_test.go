package statesync_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func lightBlockRespMsg(n int) *ssproto.Message {
	return &ssproto.Message{Sum: &ssproto.Message_LightBlockResponse{
		LightBlockResponse: &ssproto.LightBlockResponse{
			LightBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: wgtest.CommitWith(n)},
			},
		},
	}}
}

func TestSchemaForMessage_AcceptsAtCap(t *testing.T) {
	require.NoError(t, wireguard.Scan[*ssproto.Message](wgtest.Marshal(t, lightBlockRespMsg(wgtest.MaxCommitSignatures))))
}

func TestSchemaForMessage_RejectsOverCap(t *testing.T) {
	require.Error(t, wireguard.Scan[*ssproto.Message](wgtest.Marshal(t, lightBlockRespMsg(wgtest.MaxCommitSignatures+1))))
}

func TestSchemaForMessage_PassesRequest(t *testing.T) {
	msg := &ssproto.Message{Sum: &ssproto.Message_LightBlockRequest{
		LightBlockRequest: &ssproto.LightBlockRequest{Height: 42},
	}}
	require.NoError(t, wireguard.Scan[*ssproto.Message](wgtest.Marshal(t, msg)))
}
