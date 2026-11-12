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
// configured validators until staking has bonded one. It returns none when a
// finalized block failed to commit or the stored set does not add up, and
// autobahn cannot stage an empty committee.
func (a *evmOnlyApplication) GetValidators() []abci.ValidatorUpdate {
	bonded, err := a.bondedValidators()
	if err != nil {
		logger.Error("EVM-only bonded validator set is unavailable", "err", err)
		return nil
	}
	if len(bonded) == 0 {
		return slices.Clone(a.validators)
	}
	return bonded
}

// bondedValidators returns staking's bonded set once every finalized block is
// committed. It fails when one failed to commit.
func (a *evmOnlyApplication) bondedValidators() ([]abci.ValidatorUpdate, error) {
	// openSettledView only logs a failed commit, which would serve a set from
	// before the failed block, and autobahn persists the committee it stages.
	if err := a.AwaitCommits(); err != nil {
		return nil, fmt.Errorf("block commit failed: %w", err)
	}
	view := a.storage.StateDB().OpenView()
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
