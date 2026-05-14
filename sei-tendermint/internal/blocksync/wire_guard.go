package blocksync

import (
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// MaxCommitSignatures caps the number of CommitSig entries that may appear
// in each independent Commit source within an inbound Block. The bound
// mirrors types.MaxVotesCount, the same cap ValidateBasic enforces.
// Block.LastCommit and the evidence-path SignedHeader.Commit each get their
// own budget — see commitSchema and evidenceCommitSchema below.
const MaxCommitSignatures = types.MaxVotesCount

// Proto field numbers, resolved at init from the generated struct tags so
// they track .proto regenerations. A renamed or removed field panics at
// init — a silently-disabled check is worse than a loud failure.
var (
	fieldMessageBlockRequest  = wireguard.MustFieldNum((*bcproto.Message_BlockRequest)(nil), "block_request")
	fieldMessageBlockResponse = wireguard.MustFieldNum((*bcproto.Message_BlockResponse)(nil), "block_response")
	fieldBlockResponseBlock   = wireguard.MustFieldNum((*bcproto.BlockResponse)(nil), "block")
	fieldBlockEvidence        = wireguard.MustFieldNum((*tmproto.Block)(nil), "evidence")
	fieldBlockLastCommit      = wireguard.MustFieldNum((*tmproto.Block)(nil), "last_commit")
	fieldCommitSignatures     = wireguard.MustFieldNum((*tmproto.Commit)(nil), "signatures")
	fieldEvidenceListEvidence = wireguard.MustFieldNum((*tmproto.EvidenceList)(nil), "evidence")
	fieldEvidenceLCAE         = wireguard.MustFieldNum((*tmproto.Evidence_LightClientAttackEvidence)(nil), "light_client_attack_evidence")
	fieldLCAEConflictingBlock = wireguard.MustFieldNum((*tmproto.LightClientAttackEvidence)(nil), "conflicting_block")
	fieldLightBlockSignedHdr  = wireguard.MustFieldNum((*tmproto.LightBlock)(nil), "signed_header")
	fieldSignedHeaderCommit   = wireguard.MustFieldNum((*tmproto.SignedHeader)(nil), "commit")
)

// commitSchema is the Schema used for Block.LastCommit. The wireguard
// scanner keys MaxCount counters by (*Schema, field), so this pointer's
// signature budget is independent from evidenceCommitSchema's below — each
// gets its own MaxCommitSignatures-sized accumulator.
var commitSchema = &wireguard.Schema{
	Name: "Commit",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldCommitSignatures: {MaxCount: MaxCommitSignatures},
	},
}

// evidenceCommitSchema mirrors commitSchema's Rules but is a distinct value
// so its signature counter is separate. Multiple LightClientAttackEvidence
// entries within the same Block.evidence still share this one Schema, so
// their combined signatures share one budget — only Block.LastCommit gets
// its own.
var evidenceCommitSchema = &wireguard.Schema{
	Name: "Commit (evidence)",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldCommitSignatures: {MaxCount: MaxCommitSignatures},
	},
}

// signedHeaderSchema, lightBlockSchema, lightClientAttackEvidenceSchema,
// evidenceSchema, and evidenceListSchema chain a path from Block.evidence
// down to a Commit inside a LightClientAttackEvidence.

var signedHeaderSchema = &wireguard.Schema{
	Name: "SignedHeader",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldSignedHeaderCommit: {Nested: evidenceCommitSchema},
	},
}

var lightBlockSchema = &wireguard.Schema{
	Name: "LightBlock",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldLightBlockSignedHdr: {Nested: signedHeaderSchema},
	},
}

var lightClientAttackEvidenceSchema = &wireguard.Schema{
	Name: "LightClientAttackEvidence",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldLCAEConflictingBlock: {Nested: lightBlockSchema},
	},
}

// evidenceSchema descends into the LightClientAttackEvidence variant of the
// Evidence oneof. DuplicateVoteEvidence carries no repeated CommitSig
// fields, so it has no rule and the scanner walks past it.
var evidenceSchema = &wireguard.Schema{
	Name: "Evidence",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldEvidenceLCAE: {Nested: lightClientAttackEvidenceSchema},
	},
}

var evidenceListSchema = &wireguard.Schema{
	Name: "EvidenceList",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldEvidenceListEvidence: {Nested: evidenceSchema},
	},
}

// blocksyncMessageSchema runs against the wire bytes of an inbound
// blocksync.Message. Each Commit source — Block.LastCommit and the
// evidence path — is independently bounded at MaxCommitSignatures CommitSig
// entries; multiple LightClientAttackEvidence entries share the evidence
// budget.
var blocksyncMessageSchema = &wireguard.Schema{
	Name: "blocksync.Message",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldMessageBlockResponse: {Nested: &wireguard.Schema{
			Name: "BlockResponse",
			Rules: map[wireguard.Number]wireguard.Rule{
				fieldBlockResponseBlock: {Nested: &wireguard.Schema{
					Name: "Block",
					Rules: map[wireguard.Number]wireguard.Rule{
						fieldBlockEvidence:   {Nested: evidenceListSchema},
						fieldBlockLastCommit: {Nested: commitSchema},
					},
				}},
			},
		}},
	},
}

func validateBlocksyncWire(bz []byte) error {
	return wireguard.Scan(bz, blocksyncMessageSchema)
}
