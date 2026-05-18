// Package tmschemas provides reusable wireguard.Schema building blocks for
// the tendermint type hierarchy. Reactors compose these into per-channel
// schemas and install them via the channel's PreDecode hook.
//
// Each builder returns a fresh *wireguard.Schema (the scanner keys counters
// by *Schema pointer), so different callers — or different code paths
// within one caller — can get independent budgets by calling the builders
// from scratch rather than sharing one instance.
package tmschemas

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// Proto field numbers, resolved at init from the generated struct tags so
// they track .proto regenerations. A renamed or removed field panics at
// init.
var (
	FieldCommitSignatures = wireguard.MustFieldNum[tmproto.Commit]("signatures")

	FieldBlockEvidence   = wireguard.MustFieldNum[tmproto.Block]("evidence")
	FieldBlockLastCommit = wireguard.MustFieldNum[tmproto.Block]("last_commit")

	FieldProposalEvidence   = wireguard.MustFieldNum[tmproto.Proposal]("evidence")
	FieldProposalLastCommit = wireguard.MustFieldNum[tmproto.Proposal]("last_commit")

	FieldSignedHeaderCommit  = wireguard.MustFieldNum[tmproto.SignedHeader]("commit")
	FieldLightBlockSignedHdr = wireguard.MustFieldNum[tmproto.LightBlock]("signed_header")

	FieldEvidenceListEvidence = wireguard.MustFieldNum[tmproto.EvidenceList]("evidence")
	FieldEvidenceLCAE         = wireguard.MustFieldNum[tmproto.Evidence_LightClientAttackEvidence]("light_client_attack_evidence")
	FieldLCAEConflictingBlock = wireguard.MustFieldNum[tmproto.LightClientAttackEvidence]("conflicting_block")
)

// Commit returns a Schema that caps tmproto.Commit.signatures at maxSigs.
func Commit(maxSigs int) *wireguard.Schema {
	return &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldCommitSignatures: {MaxCount: maxSigs},
		},
	}
}

// SignedHeader returns a Schema descending into SignedHeader.commit and
// applying commitLeaf there.
func SignedHeader(commitLeaf *wireguard.Schema) *wireguard.Schema {
	return &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldSignedHeaderCommit: {Nested: utils.Some(commitLeaf)},
		},
	}
}

// LightBlock returns a Schema descending into LightBlock.signed_header and
// applying signedHeaderLeaf there.
func LightBlock(signedHeaderLeaf *wireguard.Schema) *wireguard.Schema {
	return &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldLightBlockSignedHdr: {Nested: utils.Some(signedHeaderLeaf)},
		},
	}
}

// EvidenceList returns a Schema that walks the full
// EvidenceList -> Evidence -> LightClientAttackEvidence -> LightBlock
// -> SignedHeader -> Commit path, installing commitLeaf at the bottom.
// DuplicateVoteEvidence carries no repeated CommitSig fields, so it is
// walked past without descent.
func EvidenceList(commitLeaf *wireguard.Schema) *wireguard.Schema {
	sh := SignedHeader(commitLeaf)
	lb := LightBlock(sh)
	lcae := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldLCAEConflictingBlock: {Nested: utils.Some(lb)},
		},
	}
	evidence := &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldEvidenceLCAE: {Nested: utils.Some(lcae)},
		},
	}
	return &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldEvidenceListEvidence: {Nested: utils.Some(evidence)},
		},
	}
}

// Block returns a Schema descending into tmproto.Block.last_commit (with
// lastCommit) and Block.evidence (with evidenceList).
func Block(lastCommit, evidenceList *wireguard.Schema) *wireguard.Schema {
	return &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldBlockLastCommit: {Nested: utils.Some(lastCommit)},
			FieldBlockEvidence:   {Nested: utils.Some(evidenceList)},
		},
	}
}

// Proposal returns a Schema descending into tmproto.Proposal.last_commit
// and Proposal.evidence.
func Proposal(lastCommit, evidenceList *wireguard.Schema) *wireguard.Schema {
	return &wireguard.Schema{
		Rules: map[wireguard.Number]wireguard.Rule{
			FieldProposalLastCommit: {Nested: utils.Some(lastCommit)},
			FieldProposalEvidence:   {Nested: utils.Some(evidenceList)},
		},
	}
}
