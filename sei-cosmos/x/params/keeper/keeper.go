package keeper

import (
	"encoding/json"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

// Keeper of the global paramstore
type Keeper struct {
	cdc         codec.BinaryCodec
	legacyAmino *codec.LegacyAmino
	key         sdk.StoreKey
	tkey        sdk.StoreKey
	spaces      map[string]*types.Subspace
}

// NewKeeper constructs a params keeper. The disabled message type URLs it stores are limited to
// the messages registered in registry.
func NewKeeper(cdc codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey sdk.StoreKey, registry codectypes.InterfaceRegistry) Keeper {

	newKeeper := Keeper{
		cdc:         cdc,
		legacyAmino: legacyAmino,
		key:         key,
		tkey:        tkey,
		spaces:      make(map[string]*types.Subspace),
	}

	newKeeper.Subspace(types.ModuleName).WithKeyTable(types.ParamKeyTable(registry))
	return newKeeper
}

func (k Keeper) SetFeesParams(ctx sdk.Context, feesParams types.FeesParams) {
	err := feesParams.Validate()
	if err != nil {
		panic(fmt.Errorf("validating feesParams: %w", err))
	}
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
	err := json.Unmarshal(bz, &feesParams)
	if err != nil {
		panic(fmt.Errorf("marshalling feesParams: %w", err))
	}
	return feesParams
}

func (k Keeper) SetCosmosGasParams(ctx sdk.Context, cosmosGasParams types.CosmosGasParams) {
	err := cosmosGasParams.Validate()
	if err != nil {
		panic(fmt.Errorf("validating cosmosGasParams: %w", err))
	}
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
	err := json.Unmarshal(bz, &cosmosGasParams)
	if err != nil {
		panic(fmt.Errorf("marshalling cosmosGasParams: %w", err))
	}
	return cosmosGasParams
}

// SetDisabledMsgTypeURLs stores the type URLs of the messages a Cosmos transaction may not carry.
// It panics unless every entry is the type URL of a registered message.
func (k Keeper) SetDisabledMsgTypeURLs(ctx sdk.Context, typeURLs []string) {
	subspace, exist := k.GetSubspace(types.ModuleName)
	if !exist {
		panic("subspace params should exist")
	}
	if err := subspace.Validate(ctx, types.ParamStoreKeyDisabledMsgTypeURLs, typeURLs); err != nil {
		panic(fmt.Errorf("validating disabled message type URLs: %w", err))
	}
	subspace.Set(ctx, types.ParamStoreKeyDisabledMsgTypeURLs, typeURLs)
}

// GetDisabledMsgTypeURLs returns the type URLs of the messages a Cosmos transaction may not
// carry. It charges no gas and returns nil when the parameter is not stored.
func (k Keeper) GetDisabledMsgTypeURLs(ctx sdk.Context) []string {
	subspace, _ := k.GetSubspace(types.ModuleName)
	var typeURLs []string
	// The Cosmos ante handler reads this for every transaction before it installs the
	// transaction's gas meter, so a charge would land on the block's meter.
	subspace.GetIfExists(ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter()), types.ParamStoreKeyDisabledMsgTypeURLs, &typeURLs)
	return typeURLs
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
