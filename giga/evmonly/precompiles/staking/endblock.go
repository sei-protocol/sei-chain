package staking

import (
	"errors"
	"math"
	"math/big"
	"sort"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/util"
)

// EndBlock runs the SDK-free staking end-block transition.
func (p *Precompile) EndBlock(ctx *precompiles.EndBlockContext) ([]precompiles.ValidatorUpdate, error) {
	if ctx.Store == nil {
		return nil, errMissingStore
	}
	updates, err := applyAndReturnValidatorSetUpdates(ctx.Store, ctx.Block)
	if err != nil {
		return nil, err
	}
	if err := unbondAllMatureValidators(ctx.Store, ctx.Block); err != nil {
		return nil, err
	}
	if err := completeMatureUnbondings(ctx); err != nil {
		return nil, err
	}
	if err := completeMatureRedelegations(ctx.Store, ctx.Block); err != nil {
		return nil, err
	}
	return updates, nil
}

func applyAndReturnValidatorSetUpdates(store precompiles.Store, block precompiles.BlockContext) ([]precompiles.ValidatorUpdate, error) {
	params, err := loadParams(store)
	if err != nil {
		return nil, err
	}
	last, err := getLastValidatorPowers(store)
	if err != nil {
		return nil, err
	}
	candidates, err := validatorsByPower(store)
	if err != nil {
		return nil, err
	}
	maxValidators := int(params.MaxValidators)
	if maxValidators < 0 {
		maxValidators = 0
	}
	if maxValidators > len(candidates) {
		maxValidators = len(candidates)
	}

	updates := make([]precompiles.ValidatorUpdate, 0)
	totalPower := int64(0)
	amtFromBondedToNotBonded := new(big.Int)
	amtFromNotBondedToBonded := new(big.Int)

	for i := 0; i < maxValidators; i++ {
		validator := candidates[i]
		newPower, err := validatorPower(validator)
		if err != nil {
			return nil, err
		}
		if newPower == 0 {
			break
		}
		tokens, err := util.ParseAmount(validator.Tokens)
		if err != nil {
			return nil, err
		}
		switch validator.Status {
		case bondStatusUnbonded:
			validator.Status = bondStatusBonded
			amtFromNotBondedToBonded.Add(amtFromNotBondedToBonded, tokens)
		case bondStatusUnbonding:
			if err := deleteValidatorQueue(store, validator.UnbondingTime, validator.UnbondingHeight, validator.OperatorAddress); err != nil {
				return nil, err
			}
			validator.Status = bondStatusBonded
			validator.UnbondingHeight = 0
			validator.UnbondingTime = 0
			amtFromNotBondedToBonded.Add(amtFromNotBondedToBonded, tokens)
		case bondStatusBonded:
		default:
			return nil, errors.New("unexpected validator status")
		}
		if err := setValidator(store, validator); err != nil {
			return nil, err
		}

		oldPower, found := last[validator.OperatorAddress]
		if !found || oldPower != newPower {
			updates = append(updates, validatorUpdate(validator, newPower))
			if err := setLastValidatorPower(store, validator.OperatorAddress, newPower); err != nil {
				return nil, err
			}
		}
		delete(last, validator.OperatorAddress)
		if totalPower > math.MaxInt64-newPower {
			return nil, errors.New("validator power overflow")
		}
		totalPower += newPower
	}

	noLongerBonded := make([]string, 0, len(last))
	for validator := range last {
		noLongerBonded = append(noLongerBonded, validator)
	}
	sort.Strings(noLongerBonded)
	for _, validatorAddress := range noLongerBonded {
		validator, ok, err := getValidator(store, validatorAddress)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errValidatorMissing
		}
		tokens, err := util.ParseAmount(validator.Tokens)
		if err != nil {
			return nil, err
		}
		if validator.Status == bondStatusBonded {
			validator.Status = bondStatusUnbonding
			validator.UnbondingTime = util.SaturatingCompletionTime(block.Time, params.UnbondingTime)
			validator.UnbondingHeight = saturatingInt64FromUint64(block.Number)
			if err := setValidator(store, validator); err != nil {
				return nil, err
			}
			if err := insertValidatorQueue(store, validator.UnbondingTime, validator.UnbondingHeight, validator.OperatorAddress); err != nil {
				return nil, err
			}
			amtFromBondedToNotBonded.Add(amtFromBondedToNotBonded, tokens)
		}
		if err := deleteLastValidatorPower(store, validator.OperatorAddress); err != nil {
			return nil, err
		}
		updates = append(updates, validatorUpdate(validator, 0))
	}

	if amtFromNotBondedToBonded.Cmp(amtFromBondedToNotBonded) > 0 {
		delta := new(big.Int).Sub(amtFromNotBondedToBonded, amtFromBondedToNotBonded)
		if err := addPoolNotBonded(store, new(big.Int).Neg(delta)); err != nil {
			return nil, err
		}
		if err := addPoolBonded(store, delta); err != nil {
			return nil, err
		}
	} else if amtFromBondedToNotBonded.Cmp(amtFromNotBondedToBonded) > 0 {
		delta := new(big.Int).Sub(amtFromBondedToNotBonded, amtFromNotBondedToBonded)
		if err := addPoolBonded(store, new(big.Int).Neg(delta)); err != nil {
			return nil, err
		}
		if err := addPoolNotBonded(store, delta); err != nil {
			return nil, err
		}
	}

	if len(updates) > 0 {
		if err := setLastTotalPower(store, totalPower); err != nil {
			return nil, err
		}
	}
	return updates, nil
}

func validatorsByPower(store precompiles.Store) ([]Validator, error) {
	validatorAddresses, err := getStringList(store, validatorsIndexKey())
	if err != nil {
		return nil, err
	}
	validators := make([]Validator, 0, len(validatorAddresses))
	for _, validatorAddress := range validatorAddresses {
		validator, ok, err := getValidator(store, validatorAddress)
		if err != nil {
			return nil, err
		}
		if !ok || validator.Jailed {
			continue
		}
		power, err := validatorPower(validator)
		if err != nil {
			return nil, err
		}
		if power == 0 {
			continue
		}
		validators = append(validators, validator)
	}
	sort.SliceStable(validators, func(i, j int) bool {
		left, _ := validatorPower(validators[i])
		right, _ := validatorPower(validators[j])
		if left != right {
			return left > right
		}
		return validators[i].OperatorAddress < validators[j].OperatorAddress
	})
	return validators, nil
}

func validatorPower(validator Validator) (int64, error) {
	tokens, err := util.ParseAmount(validator.Tokens)
	if err != nil {
		return 0, err
	}
	if powerReduction <= 0 {
		return 0, errors.New("invalid power reduction")
	}
	power := new(big.Int).Quo(tokens, big.NewInt(powerReduction))
	if !power.IsInt64() {
		return 0, errors.New("validator power exceeds int64")
	}
	return power.Int64(), nil
}

func validatorUpdate(validator Validator, power int64) precompiles.ValidatorUpdate {
	return precompiles.ValidatorUpdate{
		PubKey: append([]byte(nil), validator.ConsensusPubkey...),
		Power:  power,
	}
}

func unbondAllMatureValidators(store precompiles.Store, block precompiles.BlockContext) error {
	ids, err := matureValidatorQueueIDs(store, block.Time, block.Number)
	if err != nil {
		return err
	}
	for _, id := range ids {
		validators, err := getStringList(store, validatorQueueKey(id))
		if err != nil {
			return err
		}
		for _, validatorAddress := range validators {
			validator, ok, err := getValidator(store, validatorAddress)
			if err != nil {
				return err
			}
			if !ok {
				return errValidatorMissing
			}
			if validator.Status != bondStatusUnbonding {
				return errors.New("validator queue contains a validator that is not unbonding")
			}
			validator.Status = bondStatusUnbonded
			if err := setValidator(store, validator); err != nil {
				return err
			}
			shares, err := util.ParseAmount(validator.DelegatorShares)
			if err != nil {
				return err
			}
			if shares.Sign() == 0 {
				if err := removeValidator(store, validator.OperatorAddress); err != nil {
					return err
				}
			}
		}
		store.Delete(validatorQueueKey(id))
		if err := removeStringListItem(store, validatorQueueIndexKey(), id); err != nil {
			return err
		}
	}
	return nil
}

func completeMatureUnbondings(ctx *precompiles.EndBlockContext) error {
	ids, err := matureTimeQueueIDs(ctx.Store, unbondingQueueIndexKey(), ctx.Block.Time)
	if err != nil {
		return err
	}
	for _, id := range ids {
		pairs, err := getDelegationPairList(ctx.Store, unbondingQueueKey(id))
		if err != nil {
			return err
		}
		for _, pair := range pairs {
			if err := completeUnbonding(ctx, pair); err != nil {
				return err
			}
		}
		ctx.Store.Delete(unbondingQueueKey(id))
		if err := removeStringListItem(ctx.Store, unbondingQueueIndexKey(), id); err != nil {
			return err
		}
	}
	return nil
}

func completeUnbonding(ctx *precompiles.EndBlockContext, pair delegationPair) error {
	record, ok, err := getUnbondingDelegation(ctx.Store, pair.DelegatorAddress, pair.ValidatorAddress)
	if err != nil || !ok {
		return err
	}
	remaining := record.Entries[:0]
	blockTime := saturatingInt64FromUint64(ctx.Block.Time)
	for _, entry := range record.Entries {
		if entry.CompletionTime > blockTime {
			remaining = append(remaining, entry)
			continue
		}
		amount, err := util.ParseAmount(entry.Balance)
		if err != nil {
			return err
		}
		if amount.Sign() != 0 {
			if err := transferStakeFromEscrowToAddress(ctx.Balances, record.DelegatorAddress, amount); err != nil {
				return err
			}
			if err := addPoolNotBonded(ctx.Store, new(big.Int).Neg(amount)); err != nil {
				return err
			}
		}
	}
	if len(remaining) == 0 {
		ctx.Store.Delete(unbondingDelegationKey(pair.DelegatorAddress, pair.ValidatorAddress))
		if err := removeStringListItem(ctx.Store, delegatorUnbondingsIndexKey(pair.DelegatorAddress), pair.ValidatorAddress); err != nil {
			return err
		}
		return removeStringListItem(ctx.Store, validatorUnbondingsIndexKey(pair.ValidatorAddress), pair.DelegatorAddress)
	}
	record.Entries = remaining
	return util.SetJSON(ctx.Store, unbondingDelegationKey(pair.DelegatorAddress, pair.ValidatorAddress), record)
}

func completeMatureRedelegations(store precompiles.Store, block precompiles.BlockContext) error {
	ids, err := matureTimeQueueIDs(store, redelegationQueueIndexKey(), block.Time)
	if err != nil {
		return err
	}
	for _, id := range ids {
		triplets, err := getRedelegationTripletList(store, redelegationQueueKey(id))
		if err != nil {
			return err
		}
		for _, triplet := range triplets {
			if err := completeRedelegation(store, triplet, block.Time); err != nil {
				return err
			}
		}
		store.Delete(redelegationQueueKey(id))
		if err := removeStringListItem(store, redelegationQueueIndexKey(), id); err != nil {
			return err
		}
	}
	return nil
}

func completeRedelegation(store precompiles.Store, triplet redelegationTriplet, blockTime uint64) error {
	record, ok, err := getRedelegation(store, triplet.DelegatorAddress, triplet.ValidatorSrcAddress, triplet.ValidatorDstAddress)
	if err != nil || !ok {
		return err
	}
	remaining := record.Entries[:0]
	completionTime := saturatingInt64FromUint64(blockTime)
	for _, entry := range record.Entries {
		if entry.CompletionTime > completionTime {
			remaining = append(remaining, entry)
		}
	}
	if len(remaining) == 0 {
		store.Delete(redelegationKey(triplet.DelegatorAddress, triplet.ValidatorSrcAddress, triplet.ValidatorDstAddress))
		return removeStringListItem(store, redelegationsIndexKey(), redelegationID(triplet.DelegatorAddress, triplet.ValidatorSrcAddress, triplet.ValidatorDstAddress))
	}
	record.Entries = remaining
	return util.SetJSON(store, redelegationKey(triplet.DelegatorAddress, triplet.ValidatorSrcAddress, triplet.ValidatorDstAddress), record)
}
