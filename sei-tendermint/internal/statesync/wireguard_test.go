package statesync_test

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const maxCommitSignatures = types.MaxVotesCount

func marshal(t *testing.T, m gogoproto.Message) []byte {
	t.Helper()
	bz, err := gogoproto.Marshal(m)
	require.NoError(t, err)
	return bz
}

func commitWith(n int) *tmproto.Commit {
	return &tmproto.Commit{Signatures: make([]tmproto.CommitSig, n)}
}

func lightBlockRespMsg(n int) *ssproto.Message {
	return &ssproto.Message{Sum: &ssproto.Message_LightBlockResponse{
		LightBlockResponse: &ssproto.LightBlockResponse{
			LightBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: commitWith(n)},
			},
		},
	}}
}

func TestSchemaForMessage_AcceptsAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*ssproto.Message](marshal(t, lightBlockRespMsg(maxCommitSignatures))))
}

func TestSchemaForMessage_RejectsOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*ssproto.Message](marshal(t, lightBlockRespMsg(maxCommitSignatures+1))))
}

func TestSchemaForMessage_PassesRequest(t *testing.T) {
	msg := &ssproto.Message{Sum: &ssproto.Message_LightBlockRequest{
		LightBlockRequest: &ssproto.LightBlockRequest{Height: 42},
	}}
	require.NoError(t, protoutils.Scan[*ssproto.Message](marshal(t, msg)))
}
