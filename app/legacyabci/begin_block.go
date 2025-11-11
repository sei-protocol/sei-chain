package legacyabci

import (
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/capability"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"

	"github.com/cosmos/cosmos-sdk/x/evidence"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"

	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"

	ibcclient "github.com/cosmos/ibc-go/v3/modules/core/02-client"
	ibckeeper "github.com/cosmos/ibc-go/v3/modules/core/keeper"
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
