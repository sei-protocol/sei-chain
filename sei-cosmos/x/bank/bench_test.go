package bank_test

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

var moduleAccAddr = authtypes.NewModuleAddress(stakingtypes.BondedPoolName)

func BenchmarkOneBankSendTxPerBlock(b *testing.B) {
	b.ReportAllocs()
	// Add an account at genesis
	acc := authtypes.BaseAccount{
		Address: addr1.String(),
	}

	// construct genesis state
	genAccs := []types.GenesisAccount{&acc}
	benchmarkApp := app.SetupWithGenesisAccounts(genAccs)
	ctx := benchmarkApp.BaseApp.NewContext(false, tmproto.Header{})

	// some value conceivably higher than the benchmarks would ever go
	require.NoError(b, apptesting.FundAccount(benchmarkApp.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 100000000000))))

	benchmarkApp.Commit(context.Background())
	txGen := app.MakeEncodingConfig().TxConfig

	// Precompute all txs
	txs, err := app.GenSequenceOfTxs(txGen, []sdk.Msg{sendMsg1}, []uint64{0}, []uint64{uint64(0)}, b.N, priv1)
	require.NoError(b, err)
	b.ResetTimer()

	height := int64(3)

	// Run this with a profiler, so its easy to distinguish what time comes from
	// Committing, and what time comes from Check/Deliver Tx.
	for i := 0; i < b.N; i++ {
		benchmarkApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: height})
		_, _, err := benchmarkApp.Check(txGen.TxEncoder(), txs[i])
		if err != nil {
			panic("something is broken in checking transaction")
		}

		_, _, err = benchmarkApp.Deliver(txGen.TxEncoder(), txs[i])
		require.NoError(b, err)
		benchmarkApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: height})
		benchmarkApp.Commit(context.Background())
		height++
	}
}

func BenchmarkOneBankMultiSendTxPerBlock(b *testing.B) {
	b.ReportAllocs()
	// Add an account at genesis
	acc := authtypes.BaseAccount{
		Address: addr1.String(),
	}

	// Construct genesis state
	genAccs := []authtypes.GenesisAccount{&acc}
	benchmarkApp := app.SetupWithGenesisAccounts(genAccs)
	ctx := benchmarkApp.BaseApp.NewContext(false, tmproto.Header{})

	// some value conceivably higher than the benchmarks would ever go
	require.NoError(b, apptesting.FundAccount(benchmarkApp.BankKeeper, ctx, addr1, sdk.NewCoins(sdk.NewInt64Coin("foocoin", 100000000000))))

	benchmarkApp.Commit(context.Background())
	txGen := app.MakeEncodingConfig().TxConfig

	// Precompute all txs
	txs, err := app.GenSequenceOfTxs(txGen, []sdk.Msg{multiSendMsg1}, []uint64{0}, []uint64{uint64(0)}, b.N, priv1)
	require.NoError(b, err)
	b.ResetTimer()

	height := int64(3)

	// Run this with a profiler, so its easy to distinguish what time comes from
	// Committing, and what time comes from Check/Deliver Tx.
	for i := 0; i < b.N; i++ {
		benchmarkApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: height})
		_, _, err := benchmarkApp.Check(txGen.TxEncoder(), txs[i])
		if err != nil {
			panic("something is broken in checking transaction")
		}

		_, _, err = benchmarkApp.Deliver(txGen.TxEncoder(), txs[i])
		require.NoError(b, err)
		benchmarkApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: height})
		benchmarkApp.Commit(context.Background())
		height++
	}
}
