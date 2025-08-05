package keeper

import (
	"encoding/json"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

// Keeper of the global paramstore
type Keeper struct {
	cdc         codec.BinaryCodec
	legacyAmino *codec.LegacyAmino
	key         sdk.StoreKey
	tkey        sdk.StoreKey
	spaces      map[string]*types.Subspace
}

// NewKeeper constructs a params keeper
func NewKeeper(cdc codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey sdk.StoreKey) Keeper {

	newKeeper := Keeper{
		cdc:         cdc,
		legacyAmino: legacyAmino,
		key:         key,
		tkey:        tkey,
		spaces:      make(map[string]*types.Subspace),
	}

	newKeeper.Subspace(types.ModuleName).WithKeyTable(types.ParamKeyTable())
	return newKeeper
}

func (k Keeper) SetFeesParams(ctx sdk.Context, feesParams types.FeesParams) {
	feesParams.Validate()
	subspace, exist := k.GetSubspace(types.ModuleName)
	if !exist {
		panic("subspace params should exist")
	}
	subspace.Set(ctx, types.ParamStoreKeyFeesParams, feesParams)
}

func (k Keeper) GetFeesParams(ctx sdk.Context) types.FeesParams {
	subspace, _ := k.GetSubspace(types.ModuleName)

	if !subspace.Has(ctx, types.ParamStoreKeyFeesParams) {
		defaultParams := *types.DefaultFeesParams()
		return defaultParams
	}

	bz := subspace.GetRaw(ctx, types.ParamStoreKeyFeesParams)
	var feesParams types.FeesParams
	json.Unmarshal(bz, &feesParams)
	return feesParams
}

func (k Keeper) SetCosmosGasParams(ctx sdk.Context, cosmosGasParams types.CosmosGasParams) {
	cosmosGasParams.Validate()
	subspace, exist := k.GetSubspace(types.ModuleName)
	if !exist {
		panic("subspace params should exist")
	}
	subspace.Set(ctx, types.ParamStoreKeyCosmosGasParams, cosmosGasParams)
}

func (k Keeper) GetCosmosGasParams(ctx sdk.Context) types.CosmosGasParams {
	subspace, _ := k.GetSubspace(types.ModuleName)

	var cosmosGasParams types.CosmosGasParams
	if !subspace.Has(ctx, types.ParamStoreKeyCosmosGasParams) {
		defaultParams := *types.DefaultCosmosGasParams()
		return defaultParams
	}

	bz := subspace.GetRaw(ctx, types.ParamStoreKeyCosmosGasParams)
	json.Unmarshal(bz, &cosmosGasParams)
	return cosmosGasParams
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+proposal.ModuleName)
}

// Allocate subspace used for keepers
func (k Keeper) Subspace(s string) types.Subspace {
	_, ok := k.spaces[s]
	if ok {
		panic("subspace already occupied")
	}

	if s == "" {
		panic("cannot use empty string for subspace")
	}

	space := types.NewSubspace(k.cdc, k.legacyAmino, k.key, k.tkey, s)
	k.spaces[s] = &space
	return space
}

// Get existing substore from keeper
func (k Keeper) GetSubspace(s string) (types.Subspace, bool) {
	space, ok := k.spaces[s]
	if !ok {
		return types.Subspace{}, false
	}
	return *space, ok
}
