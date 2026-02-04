package processblock

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (a *App) NewValidator() sdk.ValAddress {
	ctx := a.Ctx()
	key := GenerateRandomPubKey()
	address := key.Address()
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, sdk.AccAddress(address)))
	valAddress := sdk.ValAddress(address)
	validator, err := stakingtypes.NewValidator(valAddress, key, stakingtypes.NewDescription(
		generateRandomStringOfLength(4),
		generateRandomStringOfLength(4),
		generateRandomStringOfLength(8),
		generateRandomStringOfLength(8),
		generateRandomStringOfLength(16),
	))
	if err != nil {
		panic(err)
	}
	a.StakingKeeper.SetValidator(ctx, validator)
	if err := a.StakingKeeper.SetValidatorByConsAddr(ctx, validator); err != nil {
		panic(err)
	}
	a.StakingKeeper.SetNewValidatorByPowerIndex(ctx, validator)
	a.StakingKeeper.AfterValidatorCreated(ctx, validator.GetOperator())
	signingInfo := slashingtypes.NewValidatorSigningInfo(
		sdk.ConsAddress(address),
		0,
		0,
		time.Unix(0, 0),
		false,
		0,
	)
	a.SlashingKeeper.SetValidatorSigningInfo(ctx, sdk.ConsAddress(address), signingInfo)
	return valAddress
}

func (a *App) NewDelegation(delegator sdk.AccAddress, validator sdk.ValAddress, amount int64) {
	ctx := a.Ctx()
	val, found := a.StakingKeeper.GetValidator(ctx, validator)
	if !found {
		panic(fmt.Sprintf("validator %s not found", validator))
	}
	_, err := a.StakingKeeper.Delegate(ctx, delegator, sdk.NewInt(amount), stakingtypes.Unbonded, val, true)
	if err != nil {
		panic(err)
	}
}
