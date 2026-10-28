package main

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios/clob"
)

func TestCLOBPlaceWorkloadRunsWithoutConflicts(t *testing.T) {
	const txsPerBlock, accounts, rounds = 12, 48, 18
	cfg, err := parseConfig([]string{
		"--metrics-addr=",
		"--blocks=1",
		"--workload=clob-place",
		"--txs-per-block=12",
		"--accounts=48",
		"--gas-price-wei=0",
		"--min-gas-price-wei=0",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(defaultCLOBTxGasLimit), cfg.txGasLimit)

	state := newGeneratedState()
	workload, err := newWorkload(cfg, state)
	require.NoError(t, err)
	require.NoError(t, workload.(scenarios.PoolSeeder).SeedAccountPool(t.Context()))
	state.Freeze()

	store := evmonly.NewMemoryStore(state)
	executor := evmonly.NewExecutor(evmonly.Config{MinGasPrice: cfg.minGasPrice, OCCWorkers: 4},
		evmonly.WithStore(store, store.EncodeChangeSet),
		evmonly.WithReceiptStore(evmonly.NewMemoryReceiptStore()))
	defer executor.Close()
	var fills, maxGas uint64
	fillID := clobEventID(t, "Fill")
	for round := uint64(0); round < rounds; round++ {
		request, err := workload.BuildBlock(t.Context(), round+2)
		require.NoError(t, err)
		result, err := executor.ExecuteBlock(t.Context(), request)
		require.NoError(t, err)
		require.True(t, result.OCCStats.Attempted)
		require.Zero(t, result.OCCStats.RerunCount, "round %d", round)
		for i, tx := range result.Txs {
			require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status, "round %d tx %d: %v", round, i, tx.Err)
			maxGas = max(maxGas, tx.GasUsed)
			for _, log := range tx.Logs {
				if log.Topics[0] == fillID {
					fills++
				}
			}
		}
	}
	require.NotZero(t, fills)
	t.Logf("fills=%d max gas used=%d", fills, maxGas)

	// Every market's base and quote totals equal what genesis credited it.
	view := store.OpenView()
	defer view.Close()
	tradersPerMarket := uint64(accounts / txsPerBlock)
	for market := uint64(0); market < txsPerBlock; market++ {
		base, quote := new(big.Int), new(big.Int)
		for round := uint64(0); round < tradersPerMarket; round++ {
			trader, err := scenarios.CLOBTrader(scenarioConfig(cfg), round, market)
			require.NoError(t, err)
			baseSlot, quoteSlot := clob.BalanceSlots(market, trader)
			avail, locked := clob.UnpackBalance(view.GetStorage(cfg.clobContract, baseSlot))
			base.Add(base, avail).Add(base, locked)
			avail, locked = clob.UnpackBalance(view.GetStorage(cfg.clobContract, quoteSlot))
			quote.Add(quote, avail).Add(quote, locked)
		}
		want := new(big.Int).Mul(new(big.Int).SetUint64(scenarios.CLOBCredit), new(big.Int).SetUint64(tradersPerMarket))
		require.Equal(t, want, base, "market %d base", market)
		require.Equal(t, want, quote, "market %d quote", market)
	}
}

func clobEventID(t *testing.T, name string) common.Hash {
	parsed, err := abi.JSON(strings.NewReader(clob.ABI()))
	require.NoError(t, err)
	return parsed.Events[name].ID
}
