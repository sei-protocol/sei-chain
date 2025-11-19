package types

import (
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

type (
	// StakingKeeper defines the staking module interface contract needed by the
	// evidence module.
	StakingKeeper interface {
		ValidatorByConsAddr(sdk.Context, seitypes.ConsAddress) stakingtypes.ValidatorI
	}

	// SlashingKeeper defines the slashing module interface contract needed by the
	// evidence module.
	SlashingKeeper interface {
		GetPubkey(sdk.Context, cryptotypes.Address) (cryptotypes.PubKey, error)
		IsTombstoned(sdk.Context, seitypes.ConsAddress) bool
		HasValidatorSigningInfo(sdk.Context, seitypes.ConsAddress) bool
		Tombstone(sdk.Context, seitypes.ConsAddress)
		Slash(sdk.Context, seitypes.ConsAddress, sdk.Dec, int64, int64)
		SlashFractionDoubleSign(sdk.Context) sdk.Dec
		Jail(sdk.Context, seitypes.ConsAddress)
		JailUntil(sdk.Context, seitypes.ConsAddress, time.Time)
	}
)
