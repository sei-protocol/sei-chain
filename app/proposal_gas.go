package app

import (
	appante "github.com/sei-protocol/sei-chain/app/ante"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// proposalGasWanted returns the contribution proposal construction and
// validation must use for tx. Native associates use the modeled paid-path cost;
// every other transaction keeps the gas reported by its normal ante path.
func (app *App) proposalGasWanted(ctx sdk.Context, tx sdk.Tx, txSize uint64, gasWanted uint64) uint64 {
	if !evmtypes.IsTxMsgAssociate(tx) {
		return gasWanted
	}

	queryCtx := ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())
	authParams := app.AccountKeeper.GetParams(queryCtx)
	cosmosGasParams := app.ParamsKeeper.GetCosmosGasParams(queryCtx)
	return appante.AssociateTxProposalGasWanted(txSize, authParams, cosmosGasParams)
}
