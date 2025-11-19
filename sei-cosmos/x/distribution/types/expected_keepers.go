package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// AccountKeeper defines the expected account keeper used for simulations (noalias)
type AccountKeeper interface {
	GetAccount(ctx sdk.Context, addr seitypes.AccAddress) types.AccountI

	GetModuleAddress(name string) seitypes.AccAddress
	GetModuleAccount(ctx sdk.Context, name string) types.ModuleAccountI

	// TODO remove with genesis 2-phases refactor https://github.com/cosmos/cosmos-sdk/issues/2862
	SetModuleAccount(sdk.Context, types.ModuleAccountI)
}

// BankKeeper defines the expected interface needed to retrieve account balances.
type BankKeeper interface {
	GetAllBalances(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
	GetBalance(ctx sdk.Context, addr seitypes.AccAddress, denom string) sdk.Coin
	LockedCoins(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins
	SpendableCoins(ctx sdk.Context, addr seitypes.AccAddress) sdk.Coins

	SendCoinsFromModuleToModule(ctx sdk.Context, senderModule string, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx sdk.Context, senderModule string, recipientAddr seitypes.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx sdk.Context, senderAddr seitypes.AccAddress, recipientModule string, amt sdk.Coins) error
}

// StakingKeeper expected staking keeper (noalias)
type StakingKeeper interface {
	// iterate through validators by operator address, execute func for each validator
	IterateValidators(sdk.Context,
		func(index int64, validator stakingtypes.ValidatorI) (stop bool))

	// iterate through bonded validators by operator address, execute func for each validator
	IterateBondedValidatorsByPower(sdk.Context,
		func(index int64, validator stakingtypes.ValidatorI) (stop bool))

	// iterate through the consensus validator set of the last block by operator address, execute func for each validator
	IterateLastValidators(sdk.Context,
		func(index int64, validator stakingtypes.ValidatorI) (stop bool))

	Validator(sdk.Context, seitypes.ValAddress) stakingtypes.ValidatorI            // get a particular validator by operator address
	ValidatorByConsAddr(sdk.Context, seitypes.ConsAddress) stakingtypes.ValidatorI // get a particular validator by consensus address

	// slash the validator and delegators of the validator, specifying offence height, offence power, and slash fraction
	Slash(sdk.Context, seitypes.ConsAddress, int64, int64, sdk.Dec)
	Jail(sdk.Context, seitypes.ConsAddress)   // jail a validator
	Unjail(sdk.Context, seitypes.ConsAddress) // unjail a validator

	// Delegation allows for getting a particular delegation for a given validator
	// and delegator outside the scope of the staking module.
	Delegation(sdk.Context, seitypes.AccAddress, seitypes.ValAddress) stakingtypes.DelegationI

	// MaxValidators returns the maximum amount of bonded validators
	MaxValidators(sdk.Context) uint32

	IterateDelegations(ctx sdk.Context, delegator seitypes.AccAddress,
		fn func(index int64, delegation stakingtypes.DelegationI) (stop bool))

	GetLastTotalPower(ctx sdk.Context) sdk.Int
	GetLastValidatorPower(ctx sdk.Context, valAddr seitypes.ValAddress) int64

	GetAllSDKDelegations(ctx sdk.Context) []stakingtypes.Delegation
}

// StakingHooks event hooks for staking validator object (noalias)
type StakingHooks interface {
	AfterValidatorCreated(ctx sdk.Context, valAddr seitypes.ValAddress)                           // Must be called when a validator is created
	AfterValidatorRemoved(ctx sdk.Context, consAddr seitypes.ConsAddress, valAddr seitypes.ValAddress) // Must be called when a validator is deleted

	BeforeDelegationCreated(ctx sdk.Context, delAddr seitypes.AccAddress, valAddr seitypes.ValAddress)        // Must be called when a delegation is created
	BeforeDelegationSharesModified(ctx sdk.Context, delAddr seitypes.AccAddress, valAddr seitypes.ValAddress) // Must be called when a delegation's shares are modified
	AfterDelegationModified(ctx sdk.Context, delAddr seitypes.AccAddress, valAddr seitypes.ValAddress)
	BeforeValidatorSlashed(ctx sdk.Context, valAddr seitypes.ValAddress, fraction sdk.Dec)
}
