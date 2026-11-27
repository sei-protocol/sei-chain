package types_test

import (
	"testing"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	maxCommitSignatures = types.MaxVotesCount
	maxValidators       = types.MaxValidatorSetSize
	txKeySize           = crypto.HashSize
	maxEvidenceEntries  = types.MaxEvidenceEntriesPerBlock
)

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

func consensusAssembledBlock(lastCommit *tmproto.Commit, evidenceCommits ...*tmproto.Commit) *tmproto.Block {
	return &tmproto.Block{
		LastCommit: lastCommit,
		Evidence:   tmproto.EvidenceList{Evidence: evidenceList(evidenceCommits...)},
	}
}

func lcaeEvidence(n int) *tmproto.Evidence {
	ev := evidenceWithCommit(commitWith(n))
	return &ev
}

func txKeyWith(n int) *tmproto.TxKey {
	return &tmproto.TxKey{TxKey: make([]byte, n)}
}

func proposalSizeWithTxKeys(t *testing.T, n int) int {
	t.Helper()
	txKeys := make([]*tmproto.TxKey, n)
	for i := range txKeys {
		txKeys[i] = txKeyWith(txKeySize)
	}
	return len(marshal(t, &tmproto.Proposal{TxKeys: txKeys}))
}

// MaxTxKeysPerProposal must sit just above the largest tx key count the
// consensus channel can carry. The transport enforces that budget while
// reassembling a message, before the wireguard scan runs, so a cap at or above
// this point can only reject proposals that were never deliverable. The lower
// bound keeps the cap tight enough to still bound decode work.
func TestMaxTxKeysPerProposalExceedsEveryDeliverableProposal(t *testing.T) {
	require.Greater(t, proposalSizeWithTxKeys(t, types.MaxTxKeysPerProposal), types.MaxConsensusMsgBytes)
	require.LessOrEqual(t, proposalSizeWithTxKeys(t, types.MaxTxKeysPerProposal-1), types.MaxConsensusMsgBytes)
}

func TestSchemaForTxKey_AcceptsAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.TxKey](marshal(t, txKeyWith(txKeySize))))
}

func TestSchemaForTxKey_RejectsOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.TxKey](marshal(t, txKeyWith(txKeySize+1))))
}

func TestSchemaForBlock_AcceptsLastCommitAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(commitWith(maxCommitSignatures)))))
}

func TestSchemaForBlock_RejectsLastCommitOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForBlock_RejectsEvidenceOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(nil, commitWith(maxCommitSignatures+1)))))
}

func TestSchemaForBlock_LastCommitAndEvidenceHaveSeparateBudgets(t *testing.T) {
	half := maxCommitSignatures/2 + 1
	require.NoError(t, protoutils.Scan[*tmproto.Block](marshal(t,
		consensusAssembledBlock(commitWith(half), commitWith(half)))))
}

func TestSchemaForEvidence_AcceptsAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeEvidence(maxCommitSignatures))))
}

func TestSchemaForEvidence_RejectsOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeEvidence(maxCommitSignatures+1))))
}

func TestSchemaForEvidence_PassesDuplicateVoteEvidence(t *testing.T) {
	ev := &tmproto.Evidence{Sum: &tmproto.Evidence_DuplicateVoteEvidence{
		DuplicateVoteEvidence: &tmproto.DuplicateVoteEvidence{},
	}}
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, ev)))
}

func validatorsWith(n int) []*tmproto.Validator {
	out := make([]*tmproto.Validator, n)
	for i := range out {
		out[i] = &tmproto.Validator{}
	}
	return out
}

func blockWithEvidenceEntries(n int) *tmproto.Block {
	return &tmproto.Block{Evidence: tmproto.EvidenceList{Evidence: make([]tmproto.Evidence, n)}}
}

func lcaeWithByzantineValidators(n int) *tmproto.Evidence {
	return &tmproto.Evidence{Sum: &tmproto.Evidence_LightClientAttackEvidence{
		LightClientAttackEvidence: &tmproto.LightClientAttackEvidence{
			ByzantineValidators: validatorsWith(n),
		},
	}}
}

func lcaeWithValidatorSet(n int) *tmproto.Evidence {
	return &tmproto.Evidence{Sum: &tmproto.Evidence_LightClientAttackEvidence{
		LightClientAttackEvidence: &tmproto.LightClientAttackEvidence{
			ConflictingBlock: &tmproto.LightBlock{
				ValidatorSet: &tmproto.ValidatorSet{Validators: validatorsWith(n)},
			},
		},
	}}
}

// smallestValidEvidenceEntryBytes is the encoded size of the smallest evidence
// list entry that passes validation: a DuplicateVoteEvidence whose two votes
// carry only the required address, with one of them voting for a block.
func smallestValidEvidenceEntryBytes(t *testing.T) int {
	t.Helper()
	addr := make([]byte, crypto.AddressSize)
	hash := make([]byte, crypto.HashSize)
	ts := time.Unix(0, 0).UTC()
	ev := tmproto.Evidence{Sum: &tmproto.Evidence_DuplicateVoteEvidence{
		DuplicateVoteEvidence: &tmproto.DuplicateVoteEvidence{
			VoteA: &tmproto.Vote{Type: tmproto.PrevoteType, ValidatorAddress: addr, Timestamp: ts},
			VoteB: &tmproto.Vote{
				Type:             tmproto.PrevoteType,
				ValidatorAddress: addr,
				Timestamp:        ts,
				BlockID: tmproto.BlockID{
					Hash:          hash,
					PartSetHeader: tmproto.PartSetHeader{Total: 1, Hash: hash},
				},
			},
			Timestamp: ts,
		},
	}}
	_, err := types.EvidenceFromProto(&ev)
	require.NoError(t, err)
	return (&tmproto.EvidenceList{Evidence: []tmproto.Evidence{ev}}).Size()
}

// The per-block entry cap must hold every list the default evidence budget allows.
func TestMaxEvidenceEntriesCoversDefaultEvidenceBudget(t *testing.T) {
	budget := types.DefaultEvidenceParams().MaxBytes
	require.GreaterOrEqual(t, int64(maxEvidenceEntries), budget/int64(smallestValidEvidenceEntryBytes(t)))
}

func TestSchemaForBlock_AcceptsEvidenceEntriesAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Block](marshal(t, blockWithEvidenceEntries(maxEvidenceEntries))))
}

func TestSchemaForBlock_RejectsEvidenceEntriesOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Block](marshal(t, blockWithEvidenceEntries(maxEvidenceEntries+1))))
}

func TestSchemaForEvidence_AcceptsByzantineValidatorsAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeWithByzantineValidators(maxValidators))))
}

func TestSchemaForEvidence_RejectsByzantineValidatorsOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeWithByzantineValidators(maxValidators+1))))
}

func TestSchemaForEvidence_AcceptsConflictingValidatorSetAtCap(t *testing.T) {
	require.NoError(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeWithValidatorSet(maxValidators))))
}

func TestSchemaForEvidence_RejectsConflictingValidatorSetOverCap(t *testing.T) {
	require.Error(t, protoutils.Scan[*tmproto.Evidence](marshal(t, lcaeWithValidatorSet(maxValidators+1))))
}
