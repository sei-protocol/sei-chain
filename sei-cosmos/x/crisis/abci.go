package crisis

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/types"
)

// check all registered invariants
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	if k.InvCheckPeriod() == 0 || ctx.BlockHeight()%int64(k.InvCheckPeriod()) != 0 { //nolint:gosec // InvCheckPeriod is a small config value, won't overflow int64
		// skip running the invariant check
		return
	}
	k.AssertInvariants(ctx)
}
