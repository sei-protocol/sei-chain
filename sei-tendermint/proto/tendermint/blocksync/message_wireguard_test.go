package blocksync_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard/wgtest"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func blocksyncResponse(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *bcproto.Message {
	return &bcproto.Message{Sum: &bcproto.Message_BlockResponse{
		BlockResponse: &bcproto.BlockResponse{
			Block: &tmproto.Block{
				LastCommit: lastCommit,
				Evidence:   tmproto.EvidenceList{Evidence: wgtest.EvidenceList(evidenceCommits...)},
			},
		},
	}}
}

func TestSchemaForMessage_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, bcproto.SchemaForMessage.Scan(wgtest.Marshal(t,
		blocksyncResponse(wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

func TestSchemaForMessage_AcceptsEvidenceCommitAtCap(t *testing.T) {
	require.NoError(t, bcproto.SchemaForMessage.Scan(wgtest.Marshal(t,
		blocksyncResponse(nil, wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

func TestSchemaForMessage_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, bcproto.SchemaForMessage.Scan(wgtest.Marshal(t,
		blocksyncResponse(wgtest.CommitWith(wgtest.MaxCommitSignatures+1)))))
}

func TestSchemaForMessage_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, bcproto.SchemaForMessage.Scan(wgtest.Marshal(t,
		blocksyncResponse(nil, wgtest.CommitWith(wgtest.MaxCommitSignatures+1)))))
}

func TestSchemaForMessage_MultipleCommitsEachAtCapPass(t *testing.T) {
	// Each Commit is checked independently (per-instance). Two commits each
	// at exactly MaxCommitSignatures must both pass.
	require.NoError(t, bcproto.SchemaForMessage.Scan(wgtest.Marshal(t,
		blocksyncResponse(wgtest.CommitWith(wgtest.MaxCommitSignatures),
			wgtest.CommitWith(wgtest.MaxCommitSignatures)))))
}

func TestSchemaForMessage_IgnoresBlockRequest(t *testing.T) {
	msg := &bcproto.Message{Sum: &bcproto.Message_BlockRequest{
		BlockRequest: &bcproto.BlockRequest{Height: 42},
	}}
	require.NoError(t, bcproto.SchemaForMessage.Scan(wgtest.Marshal(t, msg)))
}

func TestSchemaForMessage_RejectsDuplicateNonRepeatedFieldOverCap(t *testing.T) {
	// Hand-encode a last_commit with over-cap signatures. Each occurrence of
	// a length-delimited field is checked independently (per-instance), so a
	// single over-cap occurrence must still be rejected.
	commit := emptyCommitWire(wgtest.MaxCommitSignatures + 1)
	lastCommitField := wireguard.MustFieldNum[tmproto.Block]("last_commit")
	block := protowire.AppendTag(nil, lastCommitField, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)

	// Wrap in BlockResponse (field 1) then Message.block_response (field 3).
	blockResp := protowire.AppendTag(nil, 1, protowire.BytesType)
	blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
	blockResp = append(blockResp, block...)
	msg := protowire.AppendTag(nil, 3, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
	msg = append(msg, blockResp...)

	require.Error(t, bcproto.SchemaForMessage.Scan(msg))
}

// emptyCommitWire builds the wire-format bytes for a Commit with n empty
// CommitSig entries.
func emptyCommitWire(n int) []byte {
	field := wireguard.MustFieldNum[tmproto.Commit]("signatures")
	var commit []byte
	for i := 0; i < n; i++ {
		commit = protowire.AppendTag(commit, field, protowire.BytesType)
		commit = protowire.AppendVarint(commit, 0)
	}
	return commit
}
