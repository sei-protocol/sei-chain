package staking

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// BondedValidator is a member of the bonded set.
type BondedValidator struct {
	Operator        common.Address
	ConsensusPubkey common.Hash // ed25519
	Power           uint64
}

// BondedValidators returns the bonded set the last EndBlock stored, in the
// order it ranked them, or none before staking has bonded a set. It fails when
// the stored set does not add up.
func BondedValidators(state precompiles.State) ([]BondedValidator, error) {
	return bondedValidators(newStore(state))
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

// bondedValidators returns the bonded set the last EndBlock stored, with each
// member's consensus key.
func bondedValidators(s store) ([]BondedValidator, error) {
	last, err := s.lastValidators()
	if err != nil {
		return nil, err
	}
	bonded := make([]BondedValidator, len(last))
	for i, v := range last {
		pubkey := s.consensusPubkey(v.operator)
		if pubkey == (common.Hash{}) {
			return nil, fmt.Errorf("%w: %s", errUnknownValidator, v.operator)
		}
		bonded[i] = BondedValidator{Operator: v.operator, ConsensusPubkey: pubkey, Power: v.power}
	}
	return bonded, nil
}
