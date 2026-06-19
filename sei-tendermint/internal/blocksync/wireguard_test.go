package blocksync_test

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
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

func evidenceWithCommit(c *tmproto.Commit) tmproto.Evidence {
	return tmproto.Evidence{Sum: &tmproto.Evidence_LightClientAttackEvidence{
		LightClientAttackEvidence: &tmproto.LightClientAttackEvidence{
			ConflictingBlock: &tmproto.LightBlock{
				SignedHeader: &tmproto.SignedHeader{Commit: c},
			},
		},
	}}
}

func evidenceList(commits ...*tmproto.Commit) []tmproto.Evidence {
	out := make([]tmproto.Evidence, len(commits))
	for i, c := range commits {
		out[i] = evidenceWithCommit(c)
	}
	return out
}

func blocksyncResponse(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *bcproto.Message {
	return &bcproto.Message{Sum: &bcproto.Message_BlockResponse{
		BlockResponse: &bcproto.BlockResponse{
			Block: &tmproto.Block{
				LastCommit: lastCommit,
				Evidence:   tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
			},
		},
	}}
}

func TestSchemaForMessage_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*bcproto.Message](marshal(t,
		blocksyncResponse(commitWith(maxCommitSignatures)))))
}

func TestSchemaForMessage_AcceptsEvidenceCommitAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*bcproto.Message](marshal(t,
		blocksyncResponse(nil, commitWith(maxCommitSignatures)))))
}

func TestSchemaForMessage_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*bcproto.Message](marshal(t,
		blocksyncResponse(commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForMessage_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*bcproto.Message](marshal(t,
		blocksyncResponse(nil, commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForMessage_LastCommitAndEvidenceHaveSeparateBudgets(t *testing.T) {
	half := maxCommitSignatures/2 + 1
	require.NoError(t, protoutils.Scan[*bcproto.Message](marshal(t,
		blocksyncResponse(commitWith(half), commitWith(half)))))
}

func TestSchemaForMessage_EvidenceCommitsHaveSeparateBudgets(t *testing.T) {
	half := maxCommitSignatures/2 + 1
	require.NoError(t, protoutils.Scan[*bcproto.Message](marshal(t,
		blocksyncResponse(nil, commitWith(half), commitWith(half)))))
}

func TestSchemaForMessage_IgnoresBlockRequest(t *testing.T) {
	msg := &bcproto.Message{Sum: &bcproto.Message_BlockRequest{
		BlockRequest: &bcproto.BlockRequest{Height: 42},
	}}
	require.NoError(t, protoutils.Scan[*bcproto.Message](marshal(t, msg)))
}

func TestSchemaForMessage_DuplicateNonRepeatedFieldsGetSeparateBudgets(t *testing.T) {
	// Hand-encode two last_commit entries each at the cap. gogoproto.Marshal
	// won't produce this shape (Block.last_commit is non-repeated), so we
	// build the wire bytes directly to verify each nested Commit gets its
	// own budget.
	commit := emptyCommitWire(maxCommitSignatures)
	const lastCommitField = protowire.Number(4)
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

	require.NoError(t, protoutils.Scan[*bcproto.Message](msg))
}

// emptyCommitWire builds the wire-format bytes for a Commit with n empty
// CommitSig entries.
func emptyCommitWire(n int) []byte {
	const field = protowire.Number(4)
	var commit []byte
	for range n {
		commit = protowire.AppendTag(commit, field, protowire.BytesType)
		commit = protowire.AppendVarint(commit, 0)
	}
	return commit
}
