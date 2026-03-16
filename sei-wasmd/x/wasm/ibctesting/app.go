package ibctesting

import (
	"github.com/sei-protocol/sei-chain/cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/cosmos/types"
	capabilitykeeper "github.com/sei-protocol/sei-chain/cosmos/x/capability/keeper"
	stakingkeeper "github.com/sei-protocol/sei-chain/cosmos/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"

	"github.com/sei-protocol/sei-chain/cosmos/codec"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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
