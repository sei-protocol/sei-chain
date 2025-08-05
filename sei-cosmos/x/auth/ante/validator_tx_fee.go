package ante

import (
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

var BaseDenomGasPriceAmplfier = sdk.NewInt(1_000_000_000_000)

// checkTxFeeWithValidatorMinGasPrices implements the default fee logic, where the minimum price per
// unit of gas is fixed and set by each validator, can the tx priority is computed from the gas price.
func CheckTxFeeWithValidatorMinGasPrices(ctx sdk.Context, tx sdk.Tx, simulate bool, paramsKeeper paramskeeper.Keeper) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	feeCoins := feeTx.GetFee()
	feeParams := paramsKeeper.GetFeesParams(ctx)
	feeCoins = feeCoins.NonZeroAmountsOf(append([]string{sdk.DefaultBondDenom}, feeParams.GetAllowedFeeDenoms()...))
	gas := feeTx.GetGas()

	// Ensure that the provided fees meet a minimum threshold for the validator,
	// if this is a CheckTx. This is only for local mempool purposes, and thus
	// is only ran on check tx.
	if ctx.IsCheckTx() && !simulate {
		minGasPrices := GetMinimumGasPricesWantedSorted(feeParams.GetGlobalMinimumGasPrices(), ctx.MinGasPrices())
		if !minGasPrices.IsZero() {
			requiredFees := make(sdk.Coins, len(minGasPrices))

			// Determine the required fees by multiplying each required minimum gas
			// price by the gas limit, where fee = ceil(minGasPrice * gasLimit).
			glDec := sdk.NewDec(int64(gas))
			for i, gp := range minGasPrices {
				fee := gp.Amount.Mul(glDec)
				requiredFees[i] = sdk.NewCoin(gp.Denom, fee.Ceil().RoundInt())
			}

			if !feeCoins.IsAnyGTE(requiredFees) {
				return nil, 0, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "insufficient fees; got: %s required: %s", feeCoins, requiredFees)
			}
		}
	}

	// this is the lowest priority, and will be used specifically if gas limit is set to 0
	// realistically, if the gas limit IS set to 0, the tx will run out of gas anyways.
	priority := int64(0)
	if gas > 0 {
		priority = GetTxPriority(feeCoins, int64(gas))
	}
	return feeCoins, priority, nil
}

func GetMinimumGasPricesWantedSorted(globalMinimumGasPrices, validatorMinimumGasPrices sdk.DecCoins) sdk.DecCoins {
	return globalMinimumGasPrices.UnionMax(validatorMinimumGasPrices).Sort()
}

// GetTxPriority returns a naive tx priority based on the amount of the smallest denomination of the gas price
// provided in a transaction.
// If base denom is used as fee, the calculated gas price will be amplified by 10^12 to capture higher precision
// in priority differences.
// NOTE: This implementation should be used with a great consideration as it opens potential attack vectors
// where txs with multiple coins could not be prioritize as expected.
func GetTxPriority(fee sdk.Coins, gas int64) int64 {
	var priority int64
	baseDenom, err := sdk.GetBaseDenom()
	for _, c := range fee {
		p := int64(math.MaxInt64)
		var gasPrice sdk.Int
		if err == nil && baseDenom == c.Denom {
			gasPrice = c.Amount.Mul(BaseDenomGasPriceAmplfier).QuoRaw(gas)
		} else {
			gasPrice = c.Amount.QuoRaw(gas)
		}
		if gasPrice.IsInt64() {
			p = gasPrice.Int64()
		}
		if priority == 0 || p < priority {
			priority = p
		}
	}

	return priority
}
