package epoch_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	epoch "github.com/sei-protocol/sei-chain/x/epoch"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestBasic(t *testing.T) {
	t.Parallel()
	// Create a mock context and keeper
	app := app.Setup(false, false)
	appModule := epoch.NewAppModule(
		app.AppCodec(),
		app.EpochKeeper,
		app.AccountKeeper,
		app.BankKeeper,
	)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	require.Equal(t, appModule.Name(), types.ModuleName)
	appModule.RegisterCodec(app.LegacyAmino())

	require.NotNil(t, appModule.GetTxCmd())
	require.NotNil(t, appModule.GetQueryCmd())

	require.Equal(t, appModule.Route().Path(), types.RouterKey)
	require.Equal(t, appModule.EndBlock(ctx, abci.RequestEndBlock{}), []abci.ValidatorUpdate{})
}

func TestExportGenesis(t *testing.T) {
	t.Parallel()
	// Create a mock context and keeper
	app := app.Setup(false, false)
	appModule := epoch.NewAppModule(
		app.AppCodec(),
		app.EpochKeeper,
		app.AccountKeeper,
		app.BankKeeper,
	)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	require.NotNil(t, appModule.ExportGenesis(ctx, app.AppCodec()))
}

func hasEventType(ctx sdk.Context, eventType string) bool {
	for _, event := range ctx.EventManager().Events() {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func TestBeginBlock(t *testing.T) {
	t.Parallel()
	// Create a mock context and keeper
	app := app.Setup(false, false)
	appModule := epoch.NewAppModule(
		app.AppCodec(),
		app.EpochKeeper,
		app.AccountKeeper,
		app.BankKeeper,
	)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	now := time.Now()
	ctx = ctx.WithBlockTime(now)

	// Test case: new epoch should start
	lastEpoch := types.Epoch{
		GenesisTime:           now.Add(-3 * time.Hour),
		CurrentEpochStartTime: ctx.BlockTime().Add(-2 * time.Hour), // 2 hours ago
		EpochDuration:         1 * time.Hour,                       // 1 hour epochs
		CurrentEpoch:          2,
		CurrentEpochHeight:    0,
	}
	app.EpochKeeper.SetEpoch(ctx, lastEpoch)

	appModule.BeginBlock(ctx, abci.RequestBeginBlock{})
	newEpoch := app.EpochKeeper.GetEpoch(ctx)

	ctx.EventManager().Events()

	require.Equal(t, lastEpoch.CurrentEpoch+1, newEpoch.CurrentEpoch)
	require.True(t, hasEventType(ctx, types.EventTypeNewEpoch))

	// Test case: new epoch should not start yet
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithBlockTime(lastEpoch.CurrentEpochStartTime.Add(30 * time.Minute)) // only 30 minutes passed
	app.EpochKeeper.SetEpoch(ctx, lastEpoch)

	appModule.BeginBlock(ctx, abci.RequestBeginBlock{})
	newEpoch = app.EpochKeeper.GetEpoch(ctx)

	require.Equal(t, lastEpoch.CurrentEpoch, newEpoch.CurrentEpoch)
	require.False(t, hasEventType(ctx, types.EventTypeNewEpoch))
}
