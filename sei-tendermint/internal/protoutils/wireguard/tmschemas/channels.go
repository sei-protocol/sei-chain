package tmschemas

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// MaxCommitSignatures caps the number of CommitSig entries that may appear
// in each independent Commit source within an inbound channel message. The
// bound mirrors types.MaxVotesCount, the same cap ValidateBasic enforces.
// Block.LastCommit (or Proposal.last_commit) and the evidence-path
// SignedHeader.Commit each get their own budget.
const MaxCommitSignatures = types.MaxVotesCount

// TODO(wireguard): the per-channel rules below are hand-composed today.
// Move them into proto field options (e.g. (wireguard.max_count) on the
// signatures field) and emit the Schemas from a protoc plugin alongside
// `internal/hashable/plugin`, so renaming a proto field can never silently
// disable a cap and a new channel only needs an annotation. See review
// thread on #3432 for the design discussion.

// Field numbers for the per-channel wrapper messages. The leaf-side field
// numbers (Commit.signatures, Block.last_commit, etc.) live in tmschemas.go.
var (
	// blocksync.Message
	fieldBSMessageBlockResponse = wireguard.MustFieldNum[bcproto.Message_BlockResponse]("block_response")
	fieldBSBlockResponseBlock   = wireguard.MustFieldNum[bcproto.BlockResponse]("block")

	// consensus.Message
	fieldConsMessageProposal = wireguard.MustFieldNum[tmcons.Message_Proposal]("proposal")
	fieldConsProposalInner   = wireguard.MustFieldNum[tmcons.Proposal]("proposal")

	// statesync.Message
	fieldSSMessageLightBlockResp = wireguard.MustFieldNum[ssproto.Message_LightBlockResponse]("light_block_response")
	fieldSSLightBlockRespLight   = wireguard.MustFieldNum[ssproto.LightBlockResponse]("light_block")
)

// blocksyncMessageSchema runs against an inbound blocksync.Message. Each
// Commit source — Block.LastCommit and the evidence path — has its own
// MaxCommitSignatures budget. Multiple LightClientAttackEvidence entries
// share the evidence-path budget.
var blocksyncMessageSchema = &wireguard.Schema{
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldBSMessageBlockResponse: {Nested: utils.Some(&wireguard.Schema{
			Rules: map[wireguard.Number]wireguard.Rule{
				fieldBSBlockResponseBlock: {Nested: utils.Some(Block(
					Commit(MaxCommitSignatures),
					EvidenceList(Commit(MaxCommitSignatures)),
				))},
			},
		})},
	},
}

// consensusDataChannelSchema runs against an inbound consensus.Message on
// the DataChannel. Caps tmproto.Proposal.last_commit and the evidence-path
// Commit on independent budgets. BlockPart messages carry opaque chunks of
// the block proto and pass through; the reassembled bytes are checked by
// consensusAssembledBlockSchema below.
var consensusDataChannelSchema = &wireguard.Schema{
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldConsMessageProposal: {Nested: utils.Some(&wireguard.Schema{
			Rules: map[wireguard.Number]wireguard.Rule{
				fieldConsProposalInner: {Nested: utils.Some(Proposal(
					Commit(MaxCommitSignatures),
					EvidenceList(Commit(MaxCommitSignatures)),
				))},
			},
		})},
	},
}

// consensusAssembledBlockSchema runs against the bytes reassembled from
// BlockPart messages — a marshaled tmproto.Block. Same shape as
// blocksync's inner Block schema but at the root.
var consensusAssembledBlockSchema = Block(
	Commit(MaxCommitSignatures),
	EvidenceList(Commit(MaxCommitSignatures)),
)

// evidenceMessageSchema runs against an inbound tmproto.Evidence — the
// evidence channel carries an Evidence at the root. The
// LightClientAttackEvidence variant is the only one that nests a Commit.
var evidenceMessageSchema = &wireguard.Schema{
	Rules: map[wireguard.Number]wireguard.Rule{
		FieldEvidenceLCAE: {Nested: utils.Some(&wireguard.Schema{
			Rules: map[wireguard.Number]wireguard.Rule{
				FieldLCAEConflictingBlock: {Nested: utils.Some(LightBlock(
					SignedHeader(Commit(MaxCommitSignatures)),
				))},
			},
		})},
	},
}

// statesyncLightBlockChannelSchema runs against an inbound statesync.Message
// on the LightBlock channel and caps Commit signatures along the
// light_block_response path.
var statesyncLightBlockChannelSchema = &wireguard.Schema{
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldSSMessageLightBlockResp: {Nested: utils.Some(&wireguard.Schema{
			Rules: map[wireguard.Number]wireguard.Rule{
				fieldSSLightBlockRespLight: {Nested: utils.Some(LightBlock(
					SignedHeader(Commit(MaxCommitSignatures)),
				))},
			},
		})},
	},
}

// ValidateBlocksyncMessage is the PreDecode hook for the blocksync channel.
func ValidateBlocksyncMessage(bz []byte) error {
	return wireguard.Scan(bz, blocksyncMessageSchema)
}

// ValidateConsensusDataChannel is the PreDecode hook for the consensus
// DataChannel.
func ValidateConsensusDataChannel(bz []byte) error {
	return wireguard.Scan(bz, consensusDataChannelSchema)
}

// ValidateConsensusAssembledBlock scans the bytes reassembled from
// BlockPart messages before they are unmarshaled into a tmproto.Block.
func ValidateConsensusAssembledBlock(bz []byte) error {
	return wireguard.Scan(bz, consensusAssembledBlockSchema)
}

// ValidateEvidenceMessage is the PreDecode hook for the evidence channel.
func ValidateEvidenceMessage(bz []byte) error {
	return wireguard.Scan(bz, evidenceMessageSchema)
}

// ValidateStatesyncLightBlockChannel is the PreDecode hook for the
// statesync LightBlock channel.
func ValidateStatesyncLightBlockChannel(bz []byte) error {
	return wireguard.Scan(bz, statesyncLightBlockChannelSchema)
}
