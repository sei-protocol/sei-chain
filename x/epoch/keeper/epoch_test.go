package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
)

func TestEpochKeeper(t *testing.T) {
	app := app.Setup(t, false, false, false) // Your setup function here
	ctx := app.BaseApp.NewContext(false, sdk.Header{})

	// Define an epoch
	currentTime := time.Now().UTC()
	epochIn := types.Epoch{
		CurrentEpochStartTime: currentTime,
		CurrentEpochHeight:    100,
	}

	// Verify that it's equal to what is set
	app.EpochKeeper.SetEpoch(ctx, epochIn)
	epochOut := app.EpochKeeper.GetEpoch(ctx)
	require.Equal(t, epochIn, epochOut)

	// Test case: Should panic since ctx.Blocktime() is 0
	lastEpoch := types.Epoch{
		CurrentEpochStartTime: ctx.BlockTime().Add(-2 * time.Hour), // 2 hours ago
		EpochDuration:         1 * time.Hour,                       // 1 hour epochs
		CurrentEpoch:          2,
		CurrentEpochHeight:    0,
	}
	require.Panics(t, func() { app.EpochKeeper.SetEpoch(ctx, lastEpoch) })
}
