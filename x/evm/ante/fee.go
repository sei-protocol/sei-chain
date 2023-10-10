package ante

import (
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMFeeCheckDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	paramsKeeper  *paramskeeper.Keeper
	bankKeeper    bankkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func NewEVMFeeCheckDecorator(evmKeeper *evmkeeper.Keeper, paramsKeeper *paramskeeper.Keeper) *EVMFeeCheckDecorator {
	return &EVMFeeCheckDecorator{
		evmKeeper:     evmKeeper,
		paramsKeeper:  paramsKeeper,
		bankKeeper:    evmKeeper.BankKeeper(),
		accountKeeper: evmKeeper.AccountKeeper(),
	}
}

func (fc EVMFeeCheckDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// only check fee in CheckTx (similar to normal Sei tx)
	if !ctx.IsCheckTx() || simulate {
		return ctx, nil
	}

	txData, found := evmtypes.GetContextTxData(ctx)
	if !found {
		return ctx, errors.New("could not find eth tx when checking fees")
	}

	ver, ok := evmtypes.GetContextEVMVersion(ctx)
	if !ok {
		return ctx, errors.New("could not find eth version when checking fees")
	}

	// if EVM version is London or later, gas fee cap will be used to cap the overall fee consumption,
	// so we need to make sure that cap is at least as large as the required base fee
	if ver >= evmtypes.London && txData.GetGasFeeCap().Cmp(fc.getBaseFee(ctx)) < 0 {
		return ctx, errors.New("provided gas fee cap is smaller than required base fee")
	}
	// if EVM version is Cancun or later, and the transaction contains at least one blob, we need to
	// make sure the transaction carries a non-zero blob fee cap.
	if ver >= evmtypes.Cancun && len(txData.GetBlobHashes()) > 0 {
		// For now we are simply assuming excessive blob gas is 0. In the future we might change it to be
		// dynamic based on prior block usage.
		if txData.GetBlobFeeCap().Cmp(eip4844.CalcBlobFee(0)) < 0 {
			return ctx, errors.New("provided blob fee cap is smaller than required blob fee")
		}
	}

	// txData.Fee() returns fee that's calculated with EVM gas limit. To convert it to the true fee we
	// need to multiply it with the gas multiplier
	anteCharge := new(big.Int).Mul(txData.Fee(), fc.evmKeeper.GetGasMultiplier(ctx).BigInt())

	senderSeiAddr, found := evmtypes.GetContextSeiAddress(ctx)
	if !found {
		return ctx, errors.New("no address in context")
	}
	// check if the sender has enough balance to cover fees
	if fc.bankKeeper.GetBalance(ctx, senderSeiAddr, fc.evmKeeper.GetBaseDenom(ctx)).Amount.BigInt().Cmp(anteCharge) < 0 {
		return ctx, errors.New("insufficient funds to make fee payment")
	}

	// calculate the priority by dividing the total fee with the native gas limit (i.e. the effective native gas price)
	priority := new(big.Int).Quo(anteCharge, getNativeGasLimit(txData.GetGas(), fc.evmKeeper.GetGasMultiplier(ctx)))
	ctx = ctx.WithPriority(priority.Int64())

	return next(ctx, tx, simulate)
}

// currently using a static base fee = min gas price * evm gas multiplier.
// potentially change this to be dynamically determined based on block congestion
func (fc EVMFeeCheckDecorator) getBaseFee(ctx sdk.Context) *big.Int {
	// Get Sei's native gas price
	baseDenomMinGasPrice := sdk.ZeroDec()
	baseDenom := fc.evmKeeper.GetBaseDenom(ctx)
	for _, minGasPrice := range fc.paramsKeeper.GetFeesParams(ctx).GlobalMinimumGasPrices {
		if minGasPrice.Denom == baseDenom {
			baseDenomMinGasPrice = minGasPrice.Amount
			break
		}
	}
	if baseDenomMinGasPrice.IsZero() {
		return nil
	}

	// base fee = native gas limit * native min gas price
	return new(big.Int).Mul(fc.evmKeeper.GetGasMultiplier(ctx).BigInt(), baseDenomMinGasPrice.BigInt())
}

// convert EVM gas limit into Sei's native gas limit
func getNativeGasLimit(gasLimit uint64, multiplier sdk.Dec) *big.Int {
	// Mutiply gas limit with EVM multiplier to get the true gas limit
	return new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), multiplier.BigInt())
}
