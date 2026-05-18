// Package tmschemas exposes the channel-level PreDecode hooks built on top
// of the wireguard Schemas emitted by the protoc plugin from
// `(wireguard.max_count)` and `(wireguard.descend)` annotations on .proto
// fields. To extend coverage to a new repeated field, add an annotation in
// the .proto file rather than editing this package.
package tmschemas

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// MaxCommitSignatures mirrors the (wireguard.max_count) annotation on
// tmproto.Commit.signatures and the types.MaxVotesCount cap enforced by
// Commit.ValidateBasic. Tests use it; the live runtime cap is whatever the
// generated SchemaForCommit says.
const MaxCommitSignatures = types.MaxVotesCount
