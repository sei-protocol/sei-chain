package evmonlyapp

import (
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// GetValidators returns the bonded set staking snapshotted at the end of the
// latest epoch, and the configured validators until it has snapshotted one. It
// returns none once a block is known to have failed to commit or the stored set
// does not add up, and autobahn cannot stage an empty committee.
func (a *evmOnlyApplication) GetValidators() []proxy.ValidatorUpdate {
	bonded, err := a.epochValidators()
	if err != nil {
		logger.Error("EVM-only epoch validator set is unavailable", "err", err)
		return nil
	}
	if len(bonded) == 0 {
		return slices.Clone(a.validators)
	}
	return bonded
}

// epochValidators returns the bonded set staking snapshotted at the end of the
// latest epoch, after the latest finalized block, without waiting for that
// block's commit.
func (a *evmOnlyApplication) epochValidators() ([]proxy.ValidatorUpdate, error) {
	var bonded []staking.BondedValidator
	read := func(state precompiles.State) (err error) {
		bonded, err = staking.EpochValidators(state)
		return err
	}
	// Autobahn calls this before every block, so it must not hold execution
	// behind the previous block's commit.
	if executor, ok := a.settler.Load().Get(); ok {
		if err := executor.ReadLatest(read); err != nil {
			return nil, err
		}
	} else {
		view := a.storage.StateDB().OpenView()
		defer view.Close()
		if err := read(evmonly.ReadOnlyState(view)); err != nil {
			return nil, err
		}
	}
	updates := make([]proxy.ValidatorUpdate, len(bonded))
	for i, v := range bonded {
		var err error
		if updates[i], err = validatorUpdate(v); err != nil {
			return nil, err
		}
	}
	return updates, nil
}

// validatorUpdate returns bonded validator v as consensus reads it.
func validatorUpdate(v staking.BondedValidator) (proxy.ValidatorUpdate, error) {
	power, ok := utils.SafeCast[int64](v.Power)
	if !ok {
		return proxy.ValidatorUpdate{}, fmt.Errorf("validator %s power %d does not fit int64", v.Operator, v.Power)
	}
	// PublicKeyFromBytes checks only the length, and a common.Hash is always 32 bytes.
	key := utils.OrPanic1(types.PublicKeyFromBytes(v.ConsensusPubkey[:]))
	return proxy.ValidatorUpdate{PubKey: key, Power: power}, nil
}
