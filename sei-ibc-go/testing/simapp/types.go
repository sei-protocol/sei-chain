package simapp

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// App implements the common methods for a Cosmos SDK-based application
// specific blockchain.
type App interface {
	// The assigned name of the app.
	Name() string

	// The application types codec.
	// NOTE: This shoult be sealed before being returned.
	LegacyAmino() *codec.LegacyAmino

	// Application updates every begin block.
	BeginBlocker(ctx sdk.Context)

	// Application updates every end block.
	EndBlocker(ctx sdk.Context) []abci.ValidatorUpdate

	// Application update at chain (i.e app) initialization.
	InitChainer(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain

	// Loads the app at a given height.
	LoadHeight(height int64) error

	// Exports the state of the application for a genesis file.
	ExportAppStateAndValidators(
		forZeroHeight bool, jailAllowedAddrs []string,
	) (types.ExportedApp, error)

	// All the registered module account addreses.
	ModuleAccountAddrs() map[string]bool
}
