package evidence

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("cosmos", "x", "evidence")

// BeginBlocker iterates through and handles any newly discovered evidence of
// misbehavior submitted by Tendermint. Currently, only equivocation is handled.
func BeginBlocker(ctx sdk.Context, byzantineValidators []abci.Misbehavior, k keeper.Keeper) {
	beginBlockerStart := time.Now()
	defer func() {
		evidenceMetrics.beginBlockerDuration.Record(ctx.Context(), time.Since(beginBlockerStart).Seconds())
		// TODO(PLT-414): remove once evidence_begin_blocker_duration verified
		telemetry.ModuleMeasureSince(types.ModuleName, beginBlockerStart, telemetry.MetricKeyBeginBlocker)
	}()

	for _, tmEvidence := range byzantineValidators {
		switch tmEvidence.Type {
		// It's still ongoing discussion how should we treat and slash attacks with
		// premeditation. So for now we agree to treat them in the same way.
		case abci.MisbehaviorType_DUPLICATE_VOTE, abci.MisbehaviorType_LIGHT_CLIENT_ATTACK:
			evidence := types.FromABCIEvidence(abci.Evidence(tmEvidence))
			k.HandleEquivocationEvidence(ctx, evidence.(*types.Equivocation))

		default:
			logger.Error("ignored unknown evidence type", "type", tmEvidence.Type)
		}
	}
}
