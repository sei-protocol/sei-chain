package ibctesting

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/ibc-go/v3/modules/core/keeper"

	"github.com/cosmos/cosmos-sdk/codec"
	abci "github.com/tendermint/tendermint/abci/types"
)

type TestingApp interface {
	abci.Application

	// ibc-go additions
	GetBaseApp() *baseapp.BaseApp
	GetStakingKeeper() stakingkeeper.Keeper
	GetIBCKeeper() *keeper.Keeper
	GetScopedIBCKeeper() capabilitykeeper.ScopedKeeper
	GetTxConfig() client.TxConfig

	// Implemented by SimApp
	AppCodec() codec.Codec

	// Implemented by BaseApp
	LastCommitID() sdk.CommitID
	LastBlockHeight() int64
}
