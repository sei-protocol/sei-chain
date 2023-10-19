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

	if txData.GetGasFeeCap().Cmp(fc.getBaseFee(ctx)) < 0 {
		return ctx, errors.New("provided gas fee cap is smaller than required base fee")
	}
	if txData.GetGasFeeCap().Cmp(fc.getMinimumFee(ctx)) < 0 {
		return ctx, errors.New("provided gas fee cap is smaller than minimum base fee")
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

	anteCharge := txData.Fee() // this would include blob fee if it's a blob tx

	senderSeiAddr, found := evmtypes.GetContextSeiAddress(ctx)
	if !found {
		return ctx, errors.New("no address in context")
	}
	// check if the sender has enough balance to cover fees
	if fc.bankKeeper.GetBalance(ctx, senderSeiAddr, fc.evmKeeper.GetBaseDenom(ctx)).Amount.BigInt().Cmp(anteCharge) < 0 {
		return ctx, errors.New("insufficient funds to make fee payment")
	}

	// calculate the priority by dividing the total fee with the native gas limit (i.e. the effective native gas price)
	priority := new(big.Int).Quo(anteCharge, new(big.Int).SetUint64(txData.GetGas()))
	priority = new(big.Int).Quo(priority, fc.evmKeeper.GetPriorityNormalizer(ctx).RoundInt().BigInt())
	ctx = ctx.WithPriority(priority.Int64())

	return next(ctx, tx, simulate)
}

// fee per gas to be burnt
func (fc EVMFeeCheckDecorator) getBaseFee(ctx sdk.Context) *big.Int {
	return fc.evmKeeper.GetBaseFeePerGas(ctx).RoundInt().BigInt()
}

// lowest allowed fee per gas
func (fc EVMFeeCheckDecorator) getMinimumFee(ctx sdk.Context) *big.Int {
	return fc.evmKeeper.GetMinimumFeePerGas(ctx).RoundInt().BigInt()
}
