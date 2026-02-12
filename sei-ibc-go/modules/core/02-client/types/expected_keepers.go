package types

import (
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

// StakingKeeper expected staking keeper
type StakingKeeper interface {
	GetHistoricalInfo(ctx sdk.Context, height int64) (stakingtypes.HistoricalInfo, bool)
	UnbondingTime(ctx sdk.Context) time.Duration
}

// UpgradeKeeper expected upgrade keeper
type UpgradeKeeper interface {
	ClearIBCState(ctx sdk.Context, lastHeight int64)
	GetUpgradePlan(ctx sdk.Context) (plan upgradetypes.Plan, havePlan bool)
	GetUpgradedClient(ctx sdk.Context, height int64) ([]byte, bool)
	SetUpgradedClient(ctx sdk.Context, planHeight int64, bz []byte) error
	GetUpgradedConsensusState(ctx sdk.Context, lastHeight int64) ([]byte, bool)
	SetUpgradedConsensusState(ctx sdk.Context, planHeight int64, bz []byte) error
	ScheduleUpgrade(ctx sdk.Context, plan upgradetypes.Plan) error
}
