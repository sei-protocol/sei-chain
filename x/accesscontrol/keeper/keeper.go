package keeper

import (
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/accesscontrol/types"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type (
	Keeper struct {
		cdc        codec.BinaryCodec
		storeKey   sdk.StoreKey
		paramSpace paramtypes.Subspace
	}
)

func NewKeeper(
	cdc codec.Codec,
	storeKey sdk.StoreKey,
	paramSpace paramtypes.Subspace,
) Keeper {
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	return Keeper{
		cdc:        cdc,
		storeKey:   storeKey,
		paramSpace: paramSpace,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

func (k Keeper) GetResourceDepedencyMapping(ctx sdk.Context, moduleName string, messageType string) types.MessageDependencyMapping {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.GetResourceKey(moduleName, messageType))
	depdencyMapping := types.MessageDependencyMapping{}
	k.cdc.MustUnmarshal(b, &depdencyMapping)
	return depdencyMapping
}

func (k Keeper) SetResourceDepedencyMapping(
	ctx sdk.Context,
	depdencyMapping types.MessageDependencyMapping,
) {
	store := ctx.KVStore(k.storeKey)
	b := k.cdc.MustMarshal(&depdencyMapping)
	resourceKey := types.GetResourceKey(depdencyMapping.GetModuleName().String(), depdencyMapping.GetMessageType().String())
	store.Set(resourceKey, b)
}
