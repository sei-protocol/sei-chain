package keeper

import (
	"fmt"
	"reflect"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"

	clientkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/keeper"
	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectionkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/keeper"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	channelkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/keeper"
	portkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/keeper"
	porttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/types"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

var _ types.QueryServer = (*Keeper)(nil)

// Keeper defines each ICS keeper for IBC
type Keeper struct {
	// implements gRPC QueryServer interface
	types.QueryServer

	cdc codec.BinaryCodec

	ClientKeeper     clientkeeper.Keeper
	ConnectionKeeper connectionkeeper.Keeper
	ChannelKeeper    channelkeeper.Keeper
	PortKeeper       portkeeper.Keeper
	Router           *porttypes.Router

	paramSpace paramtypes.Subspace
}

// NewKeeper creates a new ibc Keeper
func NewKeeper(
	cdc codec.BinaryCodec, key sdk.StoreKey, paramSpace paramtypes.Subspace,
	stakingKeeper clienttypes.StakingKeeper, upgradeKeeper clienttypes.UpgradeKeeper,
) *Keeper {
	// register paramSpace at top level keeper
	// set KeyTable if it has not already been set
	if !paramSpace.HasKeyTable() {
		keyTable := clienttypes.ParamKeyTable()
		keyTable.RegisterParamSet(&connectiontypes.Params{})
		// register core params
		keyTable.RegisterParamSet(&types.Params{})
		paramSpace = paramSpace.WithKeyTable(keyTable)
	}

	// panic if any of the keepers passed in is empty
	if reflect.ValueOf(stakingKeeper).IsZero() {
		panic(fmt.Errorf("cannot initialize IBC keeper: empty staking keeper"))
	}

	if reflect.ValueOf(upgradeKeeper).IsZero() {
		panic(fmt.Errorf("cannot initialize IBC keeper: empty upgrade keeper"))
	}

	clientKeeper := clientkeeper.NewKeeper(cdc, key, paramSpace, stakingKeeper, upgradeKeeper)
	connectionKeeper := connectionkeeper.NewKeeper(cdc, key, paramSpace, clientKeeper)
	portKeeper := portkeeper.NewKeeper()
	channelKeeper := channelkeeper.NewKeeper(cdc, key, paramSpace, clientKeeper, connectionKeeper)

	return &Keeper{
		cdc:              cdc,
		ClientKeeper:     clientKeeper,
		ConnectionKeeper: connectionKeeper,
		ChannelKeeper:    channelKeeper,
		PortKeeper:       portKeeper,
		paramSpace:       paramSpace,
	}
}

// Codec returns the IBC module codec.
func (k Keeper) Codec() codec.BinaryCodec {
	return k.cdc
}

// SetRouter sets the Router in IBC Keeper and seals it. The method panics if
// there is an existing router that's already sealed.
func (k *Keeper) SetRouter(rtr *porttypes.Router) {
	if k.Router != nil && k.Router.Sealed() {
		panic("cannot reset a sealed router")
	}

	k.PortKeeper.Router = rtr
	k.Router = rtr
	k.Router.Seal()
}
