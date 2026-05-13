package blocksync

import (
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// MaxCommitSignatures caps the number of CommitSig entries an inbound Block's
// LastCommit may carry. The bound mirrors types.MaxVotesCount, the same cap
// ValidateBasic enforces.
const MaxCommitSignatures = types.MaxVotesCount

// Proto field numbers, resolved at init from the generated struct tags so
// they track .proto regenerations. A renamed or removed field panics at
// init — a silently-disabled check is worse than a loud failure.
var (
	fieldMessageBlockRequest  = wireguard.MustFieldNum((*bcproto.Message_BlockRequest)(nil), "block_request")
	fieldMessageBlockResponse = wireguard.MustFieldNum((*bcproto.Message_BlockResponse)(nil), "block_response")
	fieldBlockResponseBlock   = wireguard.MustFieldNum((*bcproto.BlockResponse)(nil), "block")
	fieldBlockLastCommit      = wireguard.MustFieldNum((*tmproto.Block)(nil), "last_commit")
	fieldCommitSignatures     = wireguard.MustFieldNum((*tmproto.Commit)(nil), "signatures")
)

// blocksyncMessageSchema runs against the wire bytes of an inbound
// blocksync.Message and rejects payloads whose Block.LastCommit carries
// more than MaxCommitSignatures CommitSig entries.
var blocksyncMessageSchema = &wireguard.Schema{
	Name: "blocksync.Message",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldMessageBlockResponse: {Nested: &wireguard.Schema{
			Name: "BlockResponse",
			Rules: map[wireguard.Number]wireguard.Rule{
				fieldBlockResponseBlock: {Nested: &wireguard.Schema{
					Name: "Block",
					Rules: map[wireguard.Number]wireguard.Rule{
						fieldBlockLastCommit: {Nested: commitSchema},
					},
				}},
			},
		}},
	},
}

// commitSchema is reusable wherever a tmproto.Commit appears (Block.last_commit,
// SignedHeader.commit, etc.). Today only the blocksync channel composes it;
// statesync and consensus paths can adopt the same Schema when their
// PreDecode hooks land.
var commitSchema = &wireguard.Schema{
	Name: "Commit",
	Rules: map[wireguard.Number]wireguard.Rule{
		fieldCommitSignatures: {MaxCount: MaxCommitSignatures},
	},
}

func validateBlocksyncWire(bz []byte) error {
	return wireguard.Scan(bz, blocksyncMessageSchema)
}
