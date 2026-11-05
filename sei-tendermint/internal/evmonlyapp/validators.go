package evmonlyapp

import (
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// GetValidators returns staking's bonded set in the committed state, and the
// configured validators until staking has bonded one. It returns none when the
// stored set does not add up, which autobahn cannot stage as a committee.
func (a *evmOnlyApplication) GetValidators() []abci.ValidatorUpdate {
	bonded, err := a.bondedValidators()
	if err != nil {
		logger.Error("EVM-only bonded validator set does not add up", "err", err)
		return nil
	}
	if len(bonded) == 0 {
		return slices.Clone(a.validators)
	}
	return bonded
}

// bondedValidators returns staking's bonded set in the committed state.
func (a *evmOnlyApplication) bondedValidators() ([]abci.ValidatorUpdate, error) {
	view := a.openSettledView()
	defer view.Close()
	bonded, err := staking.BondedValidators(evmonly.ReadOnlyState(view))
	if err != nil {
		return nil, err
	}
	updates := make([]abci.ValidatorUpdate, len(bonded))
	for i, v := range bonded {
		if updates[i], err = validatorUpdate(v); err != nil {
			return nil, err
		}
	}
	return updates, nil
}

// validatorUpdate returns bonded validator v as consensus reads it.
func validatorUpdate(v staking.BondedValidator) (abci.ValidatorUpdate, error) {
	power, ok := utils.SafeCast[int64](v.Power)
	if !ok {
		return abci.ValidatorUpdate{}, fmt.Errorf("validator %s power %d does not fit int64", v.Operator, v.Power)
	}
	// PublicKeyFromBytes checks only the length, and a common.Hash is always 32 bytes.
	key := utils.OrPanic1(ed25519.PublicKeyFromBytes(v.ConsensusPubkey[:]))
	return abci.ValidatorUpdate{PubKey: crypto.PubKeyToProto(key), Power: power}, nil
}
