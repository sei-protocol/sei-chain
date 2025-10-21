package factory

import (
	"context"
	"sort"
	"fmt"

	"github.com/tendermint/tendermint/types"
)

func Validator(ctx context.Context, votingPower int64) (*types.Validator, types.PrivValidator, error) {
	privVal := types.NewMockPV()
	pubKey, err := privVal.GetPubKey(ctx)
	if err != nil {
		return nil, nil, err
	}

	val := types.NewValidator(pubKey, votingPower)
	return val, privVal, nil
}

func ValidatorSet(ctx context.Context, numValidators int, votingPower int64) (*types.ValidatorSet, []types.PrivValidator) {
	var (
		valz           = make([]*types.Validator, numValidators)
		privValidators = make([]types.PrivValidator, numValidators)
	)

	for i := 0; i < numValidators; i++ {
		val, privValidator, err := Validator(ctx, votingPower)
		if err!=nil {
			panic(fmt.Errorf("Validator(): %w", err))
		}
		valz[i] = val
		privValidators[i] = privValidator
	}

	sort.Sort(types.PrivValidatorsByAddress(privValidators))

	return types.NewValidatorSet(valz), privValidators
}
