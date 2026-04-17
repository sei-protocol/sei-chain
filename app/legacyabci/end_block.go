package legacyabci

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis"
	crisiskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

type EndBlockKeepers struct {
	CrisisKeeper  *crisiskeeper.Keeper
	GovKeeper     *govkeeper.Keeper
	StakingKeeper *stakingkeeper.Keeper
	OracleKeeper  *oraclekeeper.Keeper
	EvmKeeper     *evmkeeper.Keeper
}

func EndBlock(ctx sdk.Context, height int64, blockGasUsed int64, keepers EndBlockKeepers) []abci.ValidatorUpdate {
	crisis.EndBlocker(ctx, *keepers.CrisisKeeper)
	gov.EndBlocker(ctx, *keepers.GovKeeper)
	vals := staking.EndBlocker(ctx, *keepers.StakingKeeper)
	oracle.EndBlocker(ctx, *keepers.OracleKeeper)
	keepers.EvmKeeper.EndBlock(ctx, height, blockGasUsed)
	return vals
}
