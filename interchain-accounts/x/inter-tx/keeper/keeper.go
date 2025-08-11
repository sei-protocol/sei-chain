package keeper

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/codec"
	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	capabilitykeeper "github.com/sei-protocol/sei-chain/cosmos-sdk/x/capability/keeper"
	capabilitytypes "github.com/sei-protocol/sei-chain/cosmos-sdk/x/capability/types"

	icacontrollerkeeper "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/apps/27-interchain-accounts/controller/keeper"
	host "github.com/sei-protocol/sei-chain/ibc-go/v3/modules/core/24-host"
	"github.com/sei-protocol/sei-chain/interchain-accounts/x/inter-tx/types"
)

type Keeper struct {
	cdc codec.Codec

	storeKey sdk.StoreKey

	scopedKeeper        capabilitykeeper.ScopedKeeper
	icaControllerKeeper icacontrollerkeeper.Keeper
}

func NewKeeper(cdc codec.Codec, storeKey sdk.StoreKey, iaKeeper icacontrollerkeeper.Keeper, scopedKeeper capabilitykeeper.ScopedKeeper) Keeper {
	return Keeper{
		cdc:      cdc,
		storeKey: storeKey,

		scopedKeeper:        scopedKeeper,
		icaControllerKeeper: iaKeeper,
	}
}

// Logger returns the application logger, scoped to the associated module
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s-%s", host.ModuleName, types.ModuleName))
}

// ClaimCapability claims the channel capability passed via the OnOpenChanInit callback
func (k *Keeper) ClaimCapability(ctx sdk.Context, cap *capabilitytypes.Capability, name string) error {
	return k.scopedKeeper.ClaimCapability(ctx, cap, name)
}
