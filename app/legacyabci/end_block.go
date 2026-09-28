package legacyabci

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type EndBlockKeepers struct {
	GovKeeper     *govkeeper.Keeper
	StakingKeeper *stakingkeeper.Keeper
	EvmKeeper     *evmkeeper.Keeper
}

func EndBlock(ctx sdk.Context, height int64, blockGasUsed int64, keepers EndBlockKeepers) []abci.ValidatorUpdate {
	gov.EndBlocker(ctx, *keepers.GovKeeper)
	vals := staking.EndBlocker(ctx, *keepers.StakingKeeper)
	keepers.EvmKeeper.EndBlock(ctx, height, blockGasUsed)
	return vals
}
