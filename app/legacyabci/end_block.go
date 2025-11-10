package legacyabci

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	crisiskeeper "github.com/cosmos/cosmos-sdk/x/crisis/keeper"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	abci "github.com/tendermint/tendermint/abci/types"
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
