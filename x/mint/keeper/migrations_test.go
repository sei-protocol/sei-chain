package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	typesparams "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/sei-protocol/sei-chain/x/mint/keeper"
	"github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

type MockAccountMigrationKeeper struct {
	ModuleAddress sdk.AccAddress
	ModuleAccount authtypes.ModuleAccountI
}

func (m MockAccountMigrationKeeper) GetModuleAddress(name string) sdk.AccAddress {
	address, _ := sdk.AccAddressFromBech32("sei1t4xhq2pnhnf223zr4z5lw02vsrxwf74z604kja")
	return address
}

func (m MockAccountMigrationKeeper) SetModuleAccount(ctx sdk.Context, account authtypes.ModuleAccountI) {
	m.ModuleAccount = account
}

func (m MockAccountMigrationKeeper) GetModuleAccount(ctx sdk.Context, moduleName string) authtypes.ModuleAccountI {
	return m.ModuleAccount
}

func TestMigrate2to3(t *testing.T) {

	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	paramsSubspace := typesparams.NewSubspace(cdc,
		codec.NewLegacyAmino(),
		storeKey,
		memStoreKey,
		"MintParams",
	)
	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	if !paramsSubspace.HasKeyTable() {
		paramsSubspace = paramsSubspace.WithKeyTable(paramtypes.NewKeyTable().RegisterParamSet(&types.Version2Params{}))
	}
	store := ctx.KVStore(storeKey)

	// Set up the old Minter and Params
	oldMinter := types.Version2Minter{
		LastMintAmount: sdk.NewDec(1000),
		LastMintDate:   "2021-01-01",
		LastMintHeight: 100,
		Denom:          sdk.DefaultBondDenom,
	}

	oldTokenReleaseSchedule := []types.Version2ScheduledTokenRelease{
		{
			Date:               "2021-02-01",
			TokenReleaseAmount: 500,
		},
		{
			Date:               "2021-03-01",
			TokenReleaseAmount: 1000,
		},
	}

	// Start up post upgrade with new Param Space
	newParamsSubspace := typesparams.NewSubspace(cdc,
		codec.NewLegacyAmino(),
		storeKey,
		memStoreKey,
		"MintParams",
	)
	mintKeeper := keeper.NewKeeper(
		cdc,
		storeKey,
		newParamsSubspace,
		nil,
		MockAccountMigrationKeeper{},
		nil,
		nil,
		"fee_collector",
	)

	oldParams := types.Version2Params{
		MintDenom:            sdk.DefaultBondDenom,
		TokenReleaseSchedule: oldTokenReleaseSchedule,
	}

	// Store the old Minter and Params
	b := cdc.MustMarshal(&oldMinter)
	store.Set(types.MinterKey, b)
	paramsSubspace.SetParamSet(ctx, &oldParams)

	// Perform the migration

	// Use new keeper or param space here
	migrator := keeper.NewMigrator(mintKeeper)
	err := migrator.Migrate2to3(ctx)
	require.NoError(t, err)

	// Check if the new Minter was stored correctly
	minterBytes := store.Get(types.MinterKey)
	if minterBytes == nil {
		panic("stored minter should not have been nil")
	}
	var newMinter types.Minter
	cdc.MustUnmarshal(minterBytes, &newMinter)

	require.Equal(t, oldMinter.LastMintDate, newMinter.StartDate)
	require.Equal(t, oldMinter.LastMintDate, newMinter.EndDate)
	require.Equal(t, oldMinter.LastMintDate, newMinter.LastMintDate)
	require.Equal(t, oldMinter.LastMintHeight, int64(newMinter.LastMintHeight))
	require.Equal(t, oldMinter.LastMintAmount.RoundInt().Uint64(), newMinter.TotalMintAmount)
	require.Equal(t, oldMinter.LastMintAmount.RoundInt().Uint64(), newMinter.LastMintAmount)

	// Check if the new Params were stored correctly
	var newTokenReleaseSchedules []types.ScheduledTokenRelease
	mintKeeper.GetParamSpace().Get(ctx, types.KeyTokenReleaseSchedule, &newTokenReleaseSchedules)

	var newMintDenom string
	mintKeeper.GetParamSpace().Get(ctx, types.KeyMintDenom, &newMintDenom)

	require.Equal(t, oldParams.MintDenom, newMintDenom)
	require.Len(t, newTokenReleaseSchedules, len(oldParams.TokenReleaseSchedule))

	for i, oldSchedule := range oldParams.TokenReleaseSchedule {
		newSchedule := newTokenReleaseSchedules[i]

		require.Equal(t, oldSchedule.Date, newSchedule.StartDate)
		require.Equal(t, oldSchedule.Date, newSchedule.EndDate)
		require.Equal(t, uint64(oldSchedule.TokenReleaseAmount), newSchedule.TokenReleaseAmount)
	}
}
