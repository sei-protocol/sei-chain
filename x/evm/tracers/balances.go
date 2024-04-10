package tracers

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

type BankBalanceKeeper interface {
	GetBalance(sdk.Context, sdk.AccAddress, string) sdk.Coin
	GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
}

// TraceTransactionRewards is a helper function to trace the payment of the transaction rewards
// to the coinbase address.
func TraceTransactionRewards(
	ctx sdk.Context,
	hooks *tracing.Hooks,
	bankKeeper BankBalanceKeeper,
	toSeiAddr sdk.AccAddress,
	toEVMAddr common.Address,
	usei sdk.Int,
	wei sdk.Int,
) {
	value := usei.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()

	newBalance := getEVMBalance(ctx, bankKeeper, toSeiAddr)
	oldBalance := new(big.Int).Sub(newBalance, value)

	hooks.OnBalanceChange(toEVMAddr, oldBalance, newBalance, tracing.BalanceIncreaseRewardTransactionFee)
}

func TraceTransferEVMValue(
	ctx sdk.Context,
	hooks *tracing.Hooks,
	bankKeeper BankBalanceKeeper,
	fromSeiAddr sdk.AccAddress,
	fromEVMAddr common.Address,
	toSeiAddr sdk.AccAddress,
	toEVMAddr common.Address,
	value *big.Int,
) {
	// From address got value removed from it
	newBalance := getEVMBalance(ctx, bankKeeper, fromSeiAddr)
	oldBalance := new(big.Int).Add(newBalance, value)

	hooks.OnBalanceChange(fromEVMAddr, oldBalance, newBalance, tracing.BalanceChangeTransfer)

	// To received valye from the sender
	newBalance = getEVMBalance(ctx, bankKeeper, toSeiAddr)
	oldBalance = new(big.Int).Sub(newBalance, value)

	hooks.OnBalanceChange(toEVMAddr, oldBalance, newBalance, tracing.BalanceChangeTransfer)
}

func TraceBlockReward(
	ctx sdk.Context,
	hooks *tracing.Hooks,
	bankKeeper BankBalanceKeeper,
	toSeiAddr sdk.AccAddress,
	toEVMAddr common.Address,
	usei sdk.Int,
	wei sdk.Int,
) {
	value := usei.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()

	// To received value
	newBalance := getEVMBalance(ctx, bankKeeper, toSeiAddr)
	oldBalance := new(big.Int).Sub(newBalance, value)

	hooks.OnBalanceChange(toEVMAddr, oldBalance, newBalance, tracing.BalanceIncreaseRewardMineBlock)
}

func getEVMBalance(ctx sdk.Context, bankKeeper BankBalanceKeeper, addr sdk.AccAddress) *big.Int {
	swei := bankKeeper.GetBalance(ctx, addr, sdk.MustGetBaseDenom()).Amount.Mul(state.SdkUseiToSweiMultiplier)
	wei := bankKeeper.GetWeiBalance(ctx, addr)

	return swei.Add(wei).BigInt()
}
