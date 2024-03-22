package keeper

import (
	"bytes"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
)

func (k *Keeper) VerifyBalance(ctx sdk.Context, addr common.Address) {
	useiBalance := k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr), "usei").Amount
	weiBalance := k.bankKeeper.GetWeiBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr))
	totalSeiBalance := useiBalance.Mul(sdk.NewInt(1_000_000_000_000)).Add(weiBalance).BigInt()
	ethBalance, err := k.EthClient.BalanceAt(ctx.Context(), addr, big.NewInt(k.GetReplayInitialHeight(ctx)+ctx.BlockHeight()))
	if err != nil {
		panic(err)
	}
	if totalSeiBalance.Cmp(ethBalance) != 0 {
		panic(fmt.Sprintf("difference for addr %s: sei balance is %s, eth balance is %s", addr.Hex(), totalSeiBalance, ethBalance))
	}
}

func (k *Keeper) VerifyTxResult(ctx sdk.Context, hash common.Hash) {
	localReceipt, err := k.GetReceipt(ctx, hash)
	if err != nil {
		// it's okay if remote also doesn't have receipt
		_, err = k.EthClient.TransactionReceipt(ctx.Context(), hash)
		if err == ethereum.NotFound {
			return
		}
		panic(fmt.Sprintf("missing local receipt for %s", hash.Hex()))
	}
	remoteReceipt, err := k.EthClient.TransactionReceipt(ctx.Context(), hash)
	if err != nil {
		panic(err)
	}
	if localReceipt.Status != uint32(remoteReceipt.Status) {
		panic(fmt.Sprintf("remote transaction has status %d while local has status %d", remoteReceipt.Status, localReceipt.Status))
	}
	if len(localReceipt.Logs) != len(remoteReceipt.Logs) {
		panic(fmt.Sprintf("remote transaction has %d logs while local has %d logs", len(remoteReceipt.Logs), len(localReceipt.Logs)))
	}
	for i, log := range localReceipt.Logs {
		rlog := remoteReceipt.Logs[i]
		if log.Address != rlog.Address.Hex() {
			panic(fmt.Sprintf("%d-th log has address %s on local but %s on remote", i, log.Address, rlog.Address.Hex()))
		}
		if !bytes.Equal(log.Data, rlog.Data) {
			panic(fmt.Sprintf("%d-th log has data %X on local but %X on remote", i, log.Data, rlog.Data))
		}
		if len(log.Topics) != len(rlog.Topics) {
			panic(fmt.Sprintf("%d-th log has %d topics on local but %d on remote", i, len(log.Topics), len(rlog.Topics)))
		}
		for j, topic := range log.Topics {
			rtopic := rlog.Topics[j]
			if topic != rtopic.Hex() {
				panic(fmt.Sprintf("%d-th log %d-th topic is %s on local but %s on remote", i, j, topic, rtopic.Hex()))
			}
		}
	}
}

func (k *Keeper) VerifyAccount(ctx sdk.Context, addr common.Address, accountData core.GenesisAccount) {
	code := accountData.Code
	for key, expectedState := range accountData.Storage {
		actualState := k.GetState(ctx, addr, key)
		if !bytes.Equal(actualState.Bytes(), expectedState.Bytes()) {
			panic(fmt.Sprintf("storage mismatch for address %s: expected %X, got %X", addr.Hex(), expectedState, actualState))
		}
	}
	balance := accountData.Balance
	nonce := accountData.Nonce
	if !bytes.Equal(code, k.GetCode(ctx, addr)) {
		panic(fmt.Sprintf("code mismatch for address %s", addr))
	}
	useiBalance := k.BankKeeper().GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr), "usei").Amount
	weiBalance := k.bankKeeper.GetWeiBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr))
	totalSeiBalance := useiBalance.Mul(sdk.NewInt(1_000_000_000_000)).Add(weiBalance).BigInt()
	if balance.Cmp(totalSeiBalance) != 0 {
		panic(fmt.Sprintf("balance mismatch for address %s: expected %s, got %s", addr.Hex(), balance, totalSeiBalance))
	}
	if nonce != k.GetNonce(ctx, addr) {
		panic(fmt.Sprintf("nonce mismatch for address %s: expected %d, got %d", addr.Hex(), nonce, k.GetNonce(ctx, addr)))
	}
}
