package blocksync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// commitWireBytes builds the wire-format bytes for a Commit containing
// sigCount empty Signatures entries — enough to exercise the cap without
// constructing any meaningful payload.
func commitWireBytes(sigCount int) []byte {
	var commit []byte
	for i := 0; i < sigCount; i++ {
		commit = protowire.AppendTag(commit, fieldCommitSignatures, protowire.BytesType)
		commit = protowire.AppendVarint(commit, 0)
	}
	return commit
}

// blocksyncWireBytes wraps commit bytes in the BlockResponse → Block → Commit
// framing that validateBlocksyncWire walks.
func blocksyncWireBytes(commit []byte) []byte {
	block := protowire.AppendTag(nil, fieldBlockLastCommit, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)

	blockResp := protowire.AppendTag(nil, fieldBlockResponseBlock, protowire.BytesType)
	blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
	blockResp = append(blockResp, block...)

	msg := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
	msg = append(msg, blockResp...)
	return msg
}

func TestValidateBlocksyncWire_AcceptsLegitimate(t *testing.T) {
	// Exactly at the cap is allowed; only strictly greater is rejected.
	bz := blocksyncWireBytes(commitWireBytes(types.MaxVotesCount))
	require.NoError(t, validateBlocksyncWire(bz))
}

func TestValidateBlocksyncWire_AcceptsEmpty(t *testing.T) {
	require.NoError(t, validateBlocksyncWire(nil))
	require.NoError(t, validateBlocksyncWire(blocksyncWireBytes(nil)))
}

func TestValidateBlocksyncWire_RejectsOverCap(t *testing.T) {
	bz := blocksyncWireBytes(commitWireBytes(MaxCommitSignatures + 1))
	err := validateBlocksyncWire(bz)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "exceeds max"), "got %v", err)
}

func TestValidateBlocksyncWire_IgnoresUnrelatedFields(t *testing.T) {
	// A BlockRequest carrying many CommitSig-shaped tags should not trigger
	// the check: the scanner only descends into BlockResponse.
	var blockRequest []byte
	for i := 0; i < MaxCommitSignatures+1; i++ {
		blockRequest = protowire.AppendTag(blockRequest, fieldCommitSignatures, protowire.BytesType)
		blockRequest = protowire.AppendVarint(blockRequest, 0)
	}
	msg := protowire.AppendTag(nil, fieldMessageBlockRequest, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockRequest)))
	msg = append(msg, blockRequest...)
	require.NoError(t, validateBlocksyncWire(msg))
}

func TestValidateBlocksyncWire_RejectsMalformed(t *testing.T) {
	// Length-delimited field whose declared length runs past the buffer end.
	bz := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't exist
	require.Error(t, validateBlocksyncWire(bz))
}

func TestValidateBlocksyncWire_RejectsWellOverCap(t *testing.T) {
	// Far past the cap to confirm the check short-circuits rather than
	// scanning the whole buffer.
	bz := blocksyncWireBytes(commitWireBytes(MaxCommitSignatures * 4))
	require.Error(t, validateBlocksyncWire(bz))
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

// evidenceWireBytes wraps a Commit inside the Block.evidence path:
// EvidenceList → Evidence(light_client_attack_evidence) → LightClientAttackEvidence
// → LightBlock → SignedHeader → Commit.
func evidenceWireBytes(commit []byte) []byte {
	signedHeader := protowire.AppendTag(nil, fieldSignedHeaderCommit, protowire.BytesType)
	signedHeader = protowire.AppendVarint(signedHeader, uint64(len(commit)))
	signedHeader = append(signedHeader, commit...)

	lightBlock := protowire.AppendTag(nil, fieldLightBlockSignedHdr, protowire.BytesType)
	lightBlock = protowire.AppendVarint(lightBlock, uint64(len(signedHeader)))
	lightBlock = append(lightBlock, signedHeader...)

	lcae := protowire.AppendTag(nil, fieldLCAEConflictingBlock, protowire.BytesType)
	lcae = protowire.AppendVarint(lcae, uint64(len(lightBlock)))
	lcae = append(lcae, lightBlock...)

	evidence := protowire.AppendTag(nil, fieldEvidenceLCAE, protowire.BytesType)
	evidence = protowire.AppendVarint(evidence, uint64(len(lcae)))
	evidence = append(evidence, lcae...)

	evidenceList := protowire.AppendTag(nil, fieldEvidenceListEvidence, protowire.BytesType)
	evidenceList = protowire.AppendVarint(evidenceList, uint64(len(evidence)))
	evidenceList = append(evidenceList, evidence...)
	return evidenceList
}

// blocksyncWireBytesWithEvidence wraps an EvidenceList payload in the
// BlockResponse → Block → evidence framing.
func blocksyncWireBytesWithEvidence(evidenceList []byte) []byte {
	block := protowire.AppendTag(nil, fieldBlockEvidence, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(evidenceList)))
	block = append(block, evidenceList...)

	blockResp := protowire.AppendTag(nil, fieldBlockResponseBlock, protowire.BytesType)
	blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
	blockResp = append(blockResp, block...)

	msg := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
	msg = append(msg, blockResp...)
	return msg
}

func TestValidateBlocksyncWire_RejectsOverCapViaEvidence(t *testing.T) {
	// A Commit reached through Block.Evidence's LightClientAttackEvidence
	// path counts against the same signature counter as Block.LastCommit.
	evidence := evidenceWireBytes(commitWireBytes(MaxCommitSignatures + 1))
	bz := blocksyncWireBytesWithEvidence(evidence)
	require.Error(t, validateBlocksyncWire(bz))
}

func TestValidateBlocksyncWire_LastCommitAndEvidenceHaveSeparateBudgets(t *testing.T) {
	// Each Commit source gets its own MaxCommitSignatures budget. A
	// last_commit at cap combined with an evidence-path Commit at cap is
	// permitted; only when one source's own budget is exceeded does the
	// scan reject.
	atCap := commitWireBytes(MaxCommitSignatures)
	overCap := commitWireBytes(MaxCommitSignatures + 1)

	// Both sources at their own caps: should pass.
	{
		evidence := evidenceWireBytes(atCap)
		block := protowire.AppendTag(nil, fieldBlockLastCommit, protowire.BytesType)
		block = protowire.AppendVarint(block, uint64(len(atCap)))
		block = append(block, atCap...)
		block = protowire.AppendTag(block, fieldBlockEvidence, protowire.BytesType)
		block = protowire.AppendVarint(block, uint64(len(evidence)))
		block = append(block, evidence...)
		blockResp := protowire.AppendTag(nil, fieldBlockResponseBlock, protowire.BytesType)
		blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
		blockResp = append(blockResp, block...)
		msg := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
		msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
		msg = append(msg, blockResp...)
		require.NoError(t, validateBlocksyncWire(msg))
	}

	// last_commit over its own cap: rejected even if evidence is empty.
	{
		block := protowire.AppendTag(nil, fieldBlockLastCommit, protowire.BytesType)
		block = protowire.AppendVarint(block, uint64(len(overCap)))
		block = append(block, overCap...)
		blockResp := protowire.AppendTag(nil, fieldBlockResponseBlock, protowire.BytesType)
		blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
		blockResp = append(blockResp, block...)
		msg := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
		msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
		msg = append(msg, blockResp...)
		require.Error(t, validateBlocksyncWire(msg))
	}
}

func TestValidateBlocksyncWire_EvidenceCommitsShareABudget(t *testing.T) {
	// Multiple LightClientAttackEvidence entries within the same Block
	// share the evidence-path Commit budget — two evidences each carrying
	// half-cap+1 sigs combined exceeds the single evidence-path budget.
	half := MaxCommitSignatures/2 + 1
	commit := commitWireBytes(half)
	evidenceA := evidenceWireBytes(commit)
	evidenceB := evidenceWireBytes(commit)

	// Inline two Evidence entries directly inside one EvidenceList by
	// concatenating their bytes — the EvidenceList wrapper from
	// evidenceWireBytes already adds the outer framing for one, so we
	// repeat the EvidenceList.evidence(field 1) entry twice manually.
	innerA := evidenceA[findEvidenceListInner(t, evidenceA):]
	innerB := evidenceB[findEvidenceListInner(t, evidenceB):]
	combined := protowire.AppendTag(nil, fieldEvidenceListEvidence, protowire.BytesType)
	combined = protowire.AppendVarint(combined, uint64(len(innerA)))
	combined = append(combined, innerA...)
	combined = protowire.AppendTag(combined, fieldEvidenceListEvidence, protowire.BytesType)
	combined = protowire.AppendVarint(combined, uint64(len(innerB)))
	combined = append(combined, innerB...)

	bz := blocksyncWireBytesWithEvidence(combined)
	require.Error(t, validateBlocksyncWire(bz))
}

// findEvidenceListInner returns the offset within evidenceWireBytes output
// where the Evidence value (field 1 of EvidenceList) begins, by stripping
// the outer EvidenceList wrapper.
func findEvidenceListInner(t *testing.T, bz []byte) int {
	t.Helper()
	_, _, tagLen := protowire.ConsumeTag(bz)
	require.Positive(t, tagLen)
	_, lenLen := protowire.ConsumeVarint(bz[tagLen:])
	require.Positive(t, lenLen)
	// Past EvidenceList tag+length, past Evidence tag+length, we have the
	// raw Evidence payload — but we want the *Evidence* tag+length+payload,
	// not just the payload, so callers concatenate from this offset.
	return tagLen + lenLen
}

func TestValidateBlocksyncWire_AcceptsEvidenceWithinCap(t *testing.T) {
	evidence := evidenceWireBytes(commitWireBytes(MaxCommitSignatures))
	bz := blocksyncWireBytesWithEvidence(evidence)
	require.NoError(t, validateBlocksyncWire(bz))
}

func TestFieldNumbersMatchProto(t *testing.T) {
	// Documents the resolved field numbers and catches any regression in the
	// tag parser. If proto regen renames a field, init() panics before this
	// test runs — that is the louder failure mode by design.
	require.Equal(t, protowire.Number(1), fieldMessageBlockRequest)
	require.Equal(t, protowire.Number(3), fieldMessageBlockResponse)
	require.Equal(t, protowire.Number(1), fieldBlockResponseBlock)
	require.Equal(t, protowire.Number(4), fieldBlockLastCommit)
	require.Equal(t, protowire.Number(4), fieldCommitSignatures)
}

func TestMustFieldNum_UnknownFieldPanics(t *testing.T) {
	require.PanicsWithValue(t,
		`wireguard: proto field "definitely_not_a_field" not found on Commit`,
		func() { wireguard.MustFieldNum((*tmproto.Commit)(nil), "definitely_not_a_field") })
}

func TestMustFieldNum_KnownField(t *testing.T) {
	// Sanity-check that wireguard.MustFieldNum returns the proto-declared number for a
	// non-trivial field we don't otherwise read in production.
	require.Equal(t, protowire.Number(1), wireguard.MustFieldNum((*bcproto.BlockRequest)(nil), "height"))
}
