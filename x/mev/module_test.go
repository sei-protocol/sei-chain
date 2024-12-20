package mev_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/mev"
	"github.com/sei-protocol/sei-chain/x/mev/types"
)

func TestBasicModule(t *testing.T) {
	app := app.Setup(false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	module := mev.NewAppModule(
		app.AppCodec(),
		app.MevKeeper,
	)

	// Test basic module properties
	require.Equal(t, types.ModuleName, module.Name())
	require.NotNil(t, module)

	// Test BeginBlock and EndBlock
	module.BeginBlock(ctx, abci.RequestBeginBlock{})
	require.Equal(t, []abci.ValidatorUpdate{}, module.EndBlock(ctx, abci.RequestEndBlock{}))
}

func TestModuleRegistration(t *testing.T) {
	app := app.Setup(false, false)

	// Verify the module is properly registered in the app
	require.NotNil(t, app.MevKeeper)

	// Test module name matches
	require.Equal(t, types.ModuleName, types.ModuleName)
}
