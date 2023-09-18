package ante

import (
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMFeeCheckDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	paramsKeeper  *paramskeeper.Keeper
	bankKeeper    bankkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func (fc EVMFeeCheckDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// only check fee in CheckTx (similar to normal Sei tx)
	if !ctx.IsCheckTx() || simulate {
		return ctx, nil
	}

	// evm fee has to be paid in evm base denom (i.e. usei)
	// TODO: replace this to be decided by fee market, which might require a higher min price
	baseDenomMinGasPrice := sdk.ZeroDec()
	baseDenom := fc.evmKeeper.GetBaseDenom(ctx)
	for _, minGasPrice := range fc.paramsKeeper.GetFeesParams(ctx).GlobalMinimumGasPrices {
		if minGasPrice.Denom == baseDenom {
			baseDenomMinGasPrice = minGasPrice.Amount
			break
		}
	}
	if baseDenomMinGasPrice.IsZero() {
		return ctx, errors.New("EVM transactions cannot be handled if base denom min gas price is zero")
	}

	txData, found := evmtypes.GetContextTxData(ctx)
	if !found {
		return ctx, errors.New("could not find eth tx when checking fees")
	}

	fee := sdk.NewDecFromBigInt(txData.Fee())
	gasLimit := sdk.NewDecFromBigInt(new(big.Int).SetUint64(txData.GetGas())).Mul(fc.evmKeeper.GetGasMultiplier(ctx))
	requiredFee := baseDenomMinGasPrice.Mul(gasLimit)

	if fee.LT(requiredFee) {
		return ctx, errors.New("insufficient fee")
	}

	senderSeiAddr, found := evmtypes.GetContextSeiAddress(ctx)
	if !found {
		return ctx, errors.New("no address in context")
	}
	acc := fc.accountKeeper.GetAccount(ctx, senderSeiAddr)
	if acc == nil {
		return ctx, errors.New("account does not exist")
	}
	err := authante.DeductFees(fc.bankKeeper, ctx, acc, sdk.NewCoins(sdk.NewCoin(fc.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(txData.Fee()))))
	if err != nil {
		return ctx, err
	}

	priority := fee.QuoTruncate(gasLimit).RoundInt64()
	ctx = ctx.WithPriority(priority)

	return ctx, nil
}
