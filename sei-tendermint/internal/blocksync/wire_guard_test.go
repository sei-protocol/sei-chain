package blocksync

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// marshal helpers — most tests use real proto types and Marshal them; only
// the duplicate-field and malformed-wire tests need raw wire encoding.

func marshal(t *testing.T, m gogoproto.Message) []byte {
	t.Helper()
	bz, err := gogoproto.Marshal(m)
	require.NoError(t, err)
	return bz
}

func commitWith(n int) *tmproto.Commit {
	return &tmproto.Commit{Signatures: make([]tmproto.CommitSig, n)}
}

// blockResponse builds a BlockResponse Message with the given last_commit
// and (optionally) evidences carrying LightClientAttackEvidence Commits.
func blockResponse(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *bcproto.Message {
	evidences := make([]tmproto.Evidence, len(evidenceCommits))
	for i, c := range evidenceCommits {
		evidences[i] = tmproto.Evidence{Sum: &tmproto.Evidence_LightClientAttackEvidence{
			LightClientAttackEvidence: &tmproto.LightClientAttackEvidence{
				ConflictingBlock: &tmproto.LightBlock{
					SignedHeader: &tmproto.SignedHeader{Commit: c},
				},
			},
		}}
	}
	return &bcproto.Message{Sum: &bcproto.Message_BlockResponse{
		BlockResponse: &bcproto.BlockResponse{
			Block: &tmproto.Block{
				LastCommit: lastCommit,
				Evidence:   tmproto.EvidenceList{Evidence: evidences},
			},
		},
	}}
}

func TestValidateBlocksyncWire_AcceptsLegitimate(t *testing.T) {
	require.NoError(t, validateBlocksyncWire(marshal(t, blockResponse(commitWith(types.MaxVotesCount)))))
}

func TestValidateBlocksyncWire_AcceptsEmpty(t *testing.T) {
	require.NoError(t, validateBlocksyncWire(nil))
	require.NoError(t, validateBlocksyncWire(marshal(t, blockResponse(commitWith(0)))))
}

func TestValidateBlocksyncWire_RejectsOverCap(t *testing.T) {
	require.Error(t, validateBlocksyncWire(marshal(t, blockResponse(commitWith(MaxCommitSignatures+1)))))
}

func TestValidateBlocksyncWire_RejectsWellOverCap(t *testing.T) {
	require.Error(t, validateBlocksyncWire(marshal(t, blockResponse(commitWith(MaxCommitSignatures*4)))))
}

func TestValidateBlocksyncWire_IgnoresBlockRequest(t *testing.T) {
	// The scanner only descends into BlockResponse; a BlockRequest passes.
	msg := &bcproto.Message{Sum: &bcproto.Message_BlockRequest{
		BlockRequest: &bcproto.BlockRequest{Height: 42},
	}}
	require.NoError(t, validateBlocksyncWire(marshal(t, msg)))
}

func TestValidateBlocksyncWire_RejectsOverCapViaEvidence(t *testing.T) {
	require.Error(t, validateBlocksyncWire(marshal(t, blockResponse(nil, commitWith(MaxCommitSignatures+1)))))
}

func TestValidateBlocksyncWire_AcceptsEvidenceWithinCap(t *testing.T) {
	require.NoError(t, validateBlocksyncWire(marshal(t, blockResponse(nil, commitWith(MaxCommitSignatures)))))
}

func TestValidateBlocksyncWire_LastCommitAndEvidenceHaveSeparateBudgets(t *testing.T) {
	// last_commit at cap + evidence Commit at cap is permitted (separate budgets).
	require.NoError(t, validateBlocksyncWire(marshal(t,
		blockResponse(commitWith(MaxCommitSignatures), commitWith(MaxCommitSignatures)))))
	// last_commit over its own cap is rejected even with empty evidence.
	require.Error(t, validateBlocksyncWire(marshal(t, blockResponse(commitWith(MaxCommitSignatures+1)))))
}

func TestValidateBlocksyncWire_EvidenceCommitsShareABudget(t *testing.T) {
	// Two LightClientAttackEvidence entries share the evidence-path budget.
	half := MaxCommitSignatures/2 + 1
	require.Error(t, validateBlocksyncWire(marshal(t,
		blockResponse(nil, commitWith(half), commitWith(half)))))
}

// hand-encoded helpers — only used by the next two tests, which exercise
// wire-format shapes that gogoproto.Marshal won't produce (duplicate
// non-repeated message fields, truncated length-delimited entries).

func commitWireBytes(sigCount int) []byte {
	var commit []byte
	for i := 0; i < sigCount; i++ {
		commit = protowire.AppendTag(commit, fieldCommitSignatures, protowire.BytesType)
		commit = protowire.AppendVarint(commit, 0)
	}
	return commit
}

func TestValidateBlocksyncWire_RejectsDuplicateNonRepeatedFields(t *testing.T) {
	// Two last_commit entries each at the cap should still be rejected: the
	// signature counter accumulates across both occurrences.
	commit := commitWireBytes(MaxCommitSignatures)

	block := protowire.AppendTag(nil, fieldBlockLastCommit, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)
	block = protowire.AppendTag(block, fieldBlockLastCommit, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)

	blockResp := protowire.AppendTag(nil, fieldBlockResponseBlock, protowire.BytesType)
	blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
	blockResp = append(blockResp, block...)

	msg := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
	msg = append(msg, blockResp...)

	require.Error(t, validateBlocksyncWire(msg))
}

func TestValidateBlocksyncWire_RejectsMalformed(t *testing.T) {
	// Length-delimited field whose declared length runs past the buffer end.
	bz := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't exist
	require.Error(t, validateBlocksyncWire(bz))
}

func TestFieldNumbersMatchProto(t *testing.T) {
	// Documents the resolved field numbers and catches any regression in the
	// tag parser. If proto regen renames a field, init() panics before this
	// test runs — that is the louder failure mode by design.
	require.Equal(t, protowire.Number(1), fieldMessageBlockRequest)
	require.Equal(t, protowire.Number(3), fieldMessageBlockResponse)
	require.Equal(t, protowire.Number(1), fieldBlockResponseBlock)
	require.Equal(t, protowire.Number(3), fieldBlockEvidence)
	require.Equal(t, protowire.Number(4), fieldBlockLastCommit)
	require.Equal(t, protowire.Number(4), fieldCommitSignatures)
	require.Equal(t, protowire.Number(1), fieldEvidenceListEvidence)
	require.Equal(t, protowire.Number(2), fieldEvidenceLCAE)
	require.Equal(t, protowire.Number(1), fieldLCAEConflictingBlock)
	require.Equal(t, protowire.Number(1), fieldLightBlockSignedHdr)
	require.Equal(t, protowire.Number(2), fieldSignedHeaderCommit)
}
