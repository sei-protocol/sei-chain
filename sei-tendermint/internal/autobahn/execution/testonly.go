package execution

import (
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// TestCommittees returns Committees with block committed and weights holding
// after every block.
func TestCommittees(committed atypes.RoadIndex, weights map[atypes.PublicKey]uint64) Committees {
	return Committees{}.record(committed, weights)
}
