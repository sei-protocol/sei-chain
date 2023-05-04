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

// NewMigrator returns a v3 Migrator.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	defaultParams := types.DefaultParams()
	m.keeper.paramSpace.SetParamSet(ctx, &defaultParams)
	return nil
}

func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	store := ctx.KVStore(m.keeper.storeKey)
	// Migrate Minter First
	minterBytes := store.Get(types.MinterKey)
	if minterBytes == nil {
		panic("stored minter should not have been nil")
	}

	var v2Minter types.Version2Minter
	m.keeper.cdc.MustUnmarshal(minterBytes, &v2Minter)

	v3Minter := types.Minter{
		StartDate:           v2Minter.GetLastMintDate(),
		EndDate:             v2Minter.GetLastMintDate(),
		Denom:               sdk.DefaultBondDenom,
		TotalMintAmount:     v2Minter.LastMintAmount.RoundInt().Uint64(),
		RemainingMintAmount: 0,
		LastMintDate:        v2Minter.GetLastMintDate(),
		LastMintHeight:      uint64(v2Minter.GetLastMintHeight()),
		LastMintAmount:      v2Minter.LastMintAmount.RoundInt().Uint64(),
	}
	ctx.Logger().Info("Migrating minter from v2 to v3", "v2Minter", v2Minter.String(), "v3Minter", v3Minter.String())
	m.keeper.SetMinter(ctx, v3Minter)

	// Migrate TokenReleaseSchedule

	var v2TokenReleaseSchedules []types.Version2ScheduledTokenRelease
	v2TokenReleaseSchedulesBytes := m.keeper.GetParamSpace().GetRaw(ctx, types.KeyTokenReleaseSchedule)
	err := codec.NewLegacyAmino().UnmarshalJSON(v2TokenReleaseSchedulesBytes, &v2TokenReleaseSchedules)
	if err != nil {
		panic(fmt.Sprintf("Key not found or error: %s", err))
	}

	var v2MintDenom string
	v2MintDenomBytes := m.keeper.GetParamSpace().GetRaw(ctx, types.KeyMintDenom)
	err = codec.NewLegacyAmino().UnmarshalJSON(v2MintDenomBytes, &v2MintDenom)
	if err != nil {
		panic(fmt.Sprintf("Key not found or error: %s", err))
	}
	ctx.Logger().Info("Migrating mint params from v2 to v3", "v2TokenReleaseSchedules", v2TokenReleaseSchedules, "v2MintDenom", v2MintDenom)

	v3TokenReleaseSchedule := []types.ScheduledTokenRelease{}
	for _, v2TokenReleaseSchedule := range v2TokenReleaseSchedules {
		v3Schedule := types.ScheduledTokenRelease{
			TokenReleaseAmount: uint64(v2TokenReleaseSchedule.GetTokenReleaseAmount()),
			StartDate:          v2TokenReleaseSchedule.GetDate(),
			EndDate:            v2TokenReleaseSchedule.GetDate(),
		}
		v3TokenReleaseSchedule = append(v3TokenReleaseSchedule, v3Schedule)
	}
	v3Params := types.Params{
		MintDenom:            v2MintDenom,
		TokenReleaseSchedule: v3TokenReleaseSchedule,
	}
	m.keeper.SetParams(ctx, v3Params)
	ctx.Logger().Info("Migrating mint module from v2 to v3", "v3Params", v3Params.String())

	return nil
}
