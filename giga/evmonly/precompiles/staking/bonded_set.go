package staking

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// BondedValidator is a member of the bonded set.
type BondedValidator struct {
	Operator        common.Address
	ConsensusPubkey common.Hash // ed25519
	Power           uint64
}

// EpochValidators returns the bonded set snapshotted at the end of the latest
// epoch, in the order it ranked them, or none before the first snapshot. It
// fails when the stored set does not add up.
func EpochValidators(state precompiles.State) ([]BondedValidator, error) {
	s := newStore(state)
	epoch, ok := s.latestSnapshotEpoch()
	if !ok {
		return nil, nil
	}
	return s.epochValidators(epoch)
}
