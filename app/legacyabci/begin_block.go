package legacyabci

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/capability"
	capabilitykeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution"
	distrkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/keeper"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence"
	evidencekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing"
	slashingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/keeper"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"

	ibcclient "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client"
	ibckeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/keeper"
	epochmodulekeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type BeginBlockKeepers struct {
	EpochKeeper      *epochmodulekeeper.Keeper
	UpgradeKeeper    *upgradekeeper.Keeper
	CapabilityKeeper *capabilitykeeper.Keeper
	DistrKeeper      *distrkeeper.Keeper
	SlashingKeeper   *slashingkeeper.Keeper
	EvidenceKeeper   *evidencekeeper.Keeper
	StakingKeeper    *stakingkeeper.Keeper
	IBCKeeper        *ibckeeper.Keeper
	EvmKeeper        *evmkeeper.Keeper
}

func BeginBlock(
	ctx sdk.Context,
	height int64,
	votes []abci.VoteInfo,
	byzantineValidators []abci.Misbehavior,
	keepers BeginBlockKeepers,
) {
	defer telemetry.MeasureSince(time.Now(), "module", "total_begin_block")

	keepers.EpochKeeper.BeginBlock(ctx)
	upgrade.BeginBlocker(*keepers.UpgradeKeeper, ctx)
	capability.BeginBlocker(ctx, *keepers.CapabilityKeeper)
	distribution.BeginBlocker(ctx, votes, *keepers.DistrKeeper)
	slashing.BeginBlocker(ctx, votes, *keepers.SlashingKeeper)
	evidence.BeginBlocker(ctx, byzantineValidators, *keepers.EvidenceKeeper)
	staking.BeginBlocker(ctx, *keepers.StakingKeeper)
	func() {
		defer telemetry.ModuleMeasureSince("ibc", time.Now(), telemetry.MetricKeyBeginBlocker)
		ibcclient.BeginBlocker(ctx, keepers.IBCKeeper.ClientKeeper)
	}()
	keepers.EvmKeeper.BeginBlock(ctx)
}
