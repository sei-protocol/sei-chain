package keeper

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/mint/types"
)

// Migrator is a struct for handling in-place store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a new Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 migrates from version 1 to 2
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	defaultParams := types.DefaultParams()
	m.keeper.paramSpace.SetParamSet(ctx, &defaultParams)
	return nil
}

// Migrate1to2 migrates from version 1 to 2
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	ctx.Logger().Info("Migrating mint module from v2 to v3")
	store := ctx.KVStore(m.keeper.storeKey)
	// Migrate Minter First
	minterBytes := store.Get(types.MinterKey)
	if minterBytes == nil {
		panic("stored minter should not have been nil")
	}

	var oldMinter types.Version2Minter
	m.keeper.cdc.MustUnmarshal(minterBytes, &oldMinter)

	newMinter := types.Minter{
		StartDate:           oldMinter.GetLastMintDate(),
		EndDate:             oldMinter.GetLastMintDate(),
		Denom:               sdk.DefaultBondDenom,
		TotalMintAmount:     oldMinter.LastMintAmount.RoundInt().Uint64(),
		RemainingMintAmount: 0,
		LastMintDate:        oldMinter.GetLastMintDate(),
		LastMintHeight:      uint64(oldMinter.GetLastMintHeight()),
		LastMintAmount:      oldMinter.LastMintAmount.RoundInt().Uint64(),
	}
	ctx.Logger().Info("Migrating mint module from v2 to v3", "oldMinter", oldMinter.String(), "newMinter", newMinter.String())
	m.keeper.SetMinter(ctx, newMinter)

	// Migrate TokenReleaseSchedule

	var oldTokenReleaseSchedules []types.Version2ScheduledTokenRelease
	oldTokenReleaseSchedulesBytes := m.keeper.GetParamSpace().GetRaw(ctx, types.KeyTokenReleaseSchedule)
	err := codec.NewLegacyAmino().UnmarshalJSON(oldTokenReleaseSchedulesBytes, &oldTokenReleaseSchedules)
	if err != nil {
		panic(fmt.Sprintf("Key not found or error: %s", err))
	}

	var oldMintDenom string
	oldMintDenomBytes := m.keeper.GetParamSpace().GetRaw(ctx, types.KeyMintDenom)
	err = codec.NewLegacyAmino().UnmarshalJSON(oldMintDenomBytes, &oldMintDenom)
	if err != nil {
		panic(fmt.Sprintf("Key not found or error: %s", err))
	}
	ctx.Logger().Info("Migrating mint module from v2 to v3", "oldTokenReleaseSchedules", oldTokenReleaseSchedules, "oldMintDenom", oldMintDenom)
	fmt.Println("Migrating mint module from v2 to v3", "oldTokenReleaseSchedules", oldTokenReleaseSchedules, "oldMintDenom", oldMintDenom)

	newTokenReleaseSchedule := []types.ScheduledTokenRelease{}
	for _, oldTokenReleaseSchedule := range oldTokenReleaseSchedules {
		newSchedule := types.ScheduledTokenRelease{
			TokenReleaseAmount:		uint64(oldTokenReleaseSchedule.GetTokenReleaseAmount()),
			StartDate:   			oldTokenReleaseSchedule.GetDate(),
			EndDate:   				oldTokenReleaseSchedule.GetDate(),
		}
		newTokenReleaseSchedule = append(newTokenReleaseSchedule, newSchedule)
	}
	newParams := types.Params{
		MintDenom:           oldMintDenom,
		TokenReleaseSchedule: newTokenReleaseSchedule,
	}
	m.keeper.SetParams(ctx, newParams)
	ctx.Logger().Info("Migrating mint module from v2 to v3", "newParams", newParams.String())

	return nil
}
