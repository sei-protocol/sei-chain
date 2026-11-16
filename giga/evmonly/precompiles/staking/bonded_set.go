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

// EpochValidators returns the bonded set snapshotted at the end of the latest
// epoch, in the order it ranked them, or none before the first snapshot. It
// fails when the stored set does not add up.
func EpochValidators(state precompiles.State) ([]BondedValidator, error) {
	s := newStore(state)
	epoch, ok := s.latestSnapshotEpoch()
	if !ok {
		return nil, nil
	}
	set, err := s.epochValidators(epoch)
	if err != nil {
		return nil, err
	}
	bonded := make([]BondedValidator, len(set))
	for i, v := range set {
		pubkey := s.consensusPubkey(v.operator)
		if pubkey == (common.Hash{}) {
			return nil, fmt.Errorf("%w: %s", errUnknownValidator, v.operator)
		}
		bonded[i] = BondedValidator{Operator: v.operator, ConsensusPubkey: pubkey, Power: v.power}
	}
	return bonded, nil
}
