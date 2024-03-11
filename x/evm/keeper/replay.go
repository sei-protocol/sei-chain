package keeper

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

func (k *Keeper) VerifyBalance(ctx sdk.Context, addr common.Address) {
	useiBalance := k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr), "usei").Amount
	weiBalance := k.bankKeeper.GetWeiBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr))
	totalSeiBalance := useiBalance.Mul(sdk.NewInt(1_000_000_000_000)).Add(weiBalance).BigInt()
	ethBalance, err := k.EthClient.BalanceAt(ctx.Context(), addr, big.NewInt(int64(k.EthReplayConfig.EthDataEarliestBlock)+ctx.BlockHeight()))
	if err != nil {
		panic(err)
	}
	if totalSeiBalance.Cmp(ethBalance) != 0 {
		panic(fmt.Sprintf("difference for addr %s: sei balance is %s, eth balance is %s", addr.Hex(), totalSeiBalance, ethBalance))
	}
}
