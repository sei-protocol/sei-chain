package ante

import (
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	feegrantkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant/keeper"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
)

func CosmosDeliverTxAnte(
	ctx sdk.Context,
	txConfig client.TxConfig,
	tx sdk.Tx,
	pk paramskeeper.Keeper,
	oraclek oraclekeeper.Keeper,
	ek *evmkeeper.Keeper,
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.Keeper,
	feegrantKeeper *feegrantkeeper.Keeper,
) (returnCtx sdk.Context, returnErr error) {
	if _, err := CosmosStatelessChecks(tx, ctx.BlockHeight(), ctx.ConsensusParams()); err != nil {
		return SetGasMeter(ctx, 0, pk), err
	}

	defer func() {
		if r := recover(); r != nil {
			returnErr = HandleOutofGas(r, tx.(GasTx).GetGas(), ctx.GasMeter().GasConsumed())
		}
	}()
	ctx = ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())
	isGasless, err := antedecorators.IsTxGasless(tx, ctx, oraclek, ek)
	if err != nil {
		return ctx, err
	}
	if !isGasless {
		ctx = SetGasMeter(ctx, tx.(GasTx).GetGas(), pk)
	}

	authParams := accountKeeper.GetParams(ctx)

	if err := CheckMemoLength(tx, authParams); err != nil {
		return ctx, err
	}

	ctx.GasMeter().ConsumeGas(authParams.TxSizeCostPerByte*sdk.Gas(len(ctx.TxBytes())), "txSize")

	signerAccounts, err := CheckPubKeys(ctx, tx, accountKeeper, authParams)
	if err != nil {
		return ctx, err
	}

	sigEvents, err := CheckSignatures(ctx, txConfig, tx, signerAccounts, authParams)
	if err != nil {
		return ctx, err
	}
	ctx.EventManager().EmitEvents(sigEvents)

	signerEvents, err := UpdateSigners(ctx, tx, accountKeeper, ek)
	if err != nil {
		return ctx, err
	}
	ctx.EventManager().EmitEvents(signerEvents)

	if err := ChargeFees(ctx, tx, accountKeeper, bankKeeper, feegrantKeeper, pk); err != nil {
		return ctx, err
	}

	return ctx, nil
}

func ChargeFees(ctx sdk.Context, tx sdk.Tx, accountKeeper authkeeper.AccountKeeper, bankKeeper bankkeeper.Keeper, feegrantKeeper *feegrantkeeper.Keeper, paramsKeeper paramskeeper.Keeper) error {
	feeTx := tx.(sdk.FeeTx)
	feeCoins := feeTx.GetFee()
	feeParams := paramsKeeper.GetFeesParams(ctx)
	feeCoins = feeCoins.NonZeroAmountsOf(append([]string{sdk.DefaultBondDenom}, feeParams.GetAllowedFeeDenoms()...))
	deductFeesFrom, err := chargeFees(ctx, tx, feeCoins, accountKeeper, bankKeeper, feegrantKeeper)
	if err != nil {
		return err
	}
	events := sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeTx,
			sdk.NewAttribute(sdk.AttributeKeyFee, feeCoins.String()),
			sdk.NewAttribute(sdk.AttributeKeyFeePayer, deductFeesFrom.String()),
		),
	}
	ctx.EventManager().EmitEvents(events)
	return nil
}
