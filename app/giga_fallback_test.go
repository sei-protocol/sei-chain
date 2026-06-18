package app

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

type gigaFallbackTestAcct struct {
	AccountAddress sdk.AccAddress
	PublicKey      cryptotypes.PubKey
	EvmAddress     common.Address
	EvmSigner      ethtypes.Signer
	EvmPrivateKey  *ecdsa.PrivateKey
}

func TestDeliverTxWithV2FallbackFlushesGigaBlockCache(t *testing.T) {
	blockTime := time.Now()
	validator := newGigaFallbackTestSigner(t)
	sender := newGigaFallbackTestSigner(t)
	recipient1 := newGigaFallbackTestSigner(t)
	recipient2 := newGigaFallbackTestSigner(t)
	recipient3 := newGigaFallbackTestSigner(t)

	wrapper := NewGigaTestWrapper(t, blockTime, validator.PublicKey, true, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(false)
		ba.SetConcurrencyWorkers(1)
	})
	sei := wrapper.App
	ctx := wrapper.Ctx.WithBlockHeader(tmproto.Header{
		Height:  wrapper.Ctx.BlockHeader().Height,
		ChainID: wrapper.Ctx.BlockHeader().ChainID,
		Time:    blockTime,
	})

	initialFundingWei := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000_000))
	gasPrice := big.NewInt(100_000_000_000)
	values := []*big.Int{
		big.NewInt(1_000_000_000_000),
		big.NewInt(2_000_000_000_000),
		big.NewInt(3_000_000_000_000),
	}

	sei.EvmKeeper.SetAddressMapping(ctx, sender.AccountAddress, sender.EvmAddress)
	fundWeiForGigaFallbackTest(t, sei, ctx, sender.AccountAddress, initialFundingWei)

	sei.GigaEvmKeeper.UseRegularStore = false
	sei.GigaBankKeeper.UseRegularStore = false

	ms := ctx.MultiStore().CacheMultiStore()
	blockCtx := ctx.WithMultiStore(ms)
	cache, err := newGigaBlockCache(blockCtx, &sei.GigaEvmKeeper)
	require.NoError(t, err)

	preBalance := sei.GigaEvmKeeper.GetBalance(blockCtx, sender.AccountAddress)
	_, tx1 := buildGigaFallbackTestEVMTx(t, sender, &recipient1.EvmAddress, values[0], nil, 21_000, gasPrice, 0)
	tx2Bytes, tx2 := buildGigaFallbackTestEVMTx(t, sender, &recipient2.EvmAddress, values[1], nil, 21_000, gasPrice, 1)
	_, tx3 := buildGigaFallbackTestEVMTx(t, sender, &recipient3.EvmAddress, values[2], nil, 21_000, gasPrice, 2)

	result1 := executeGigaFallbackTestTx(t, sei, blockCtx.WithTxIndex(0), tx1, cache)

	// Tx1 has flushed its tx-level cache into Giga's block-level cache. The
	// fallback bridge must publish that block-level state before V2 executes tx2.
	result2 := sei.deliverTxWithV2Fallback(blockCtx.WithTxIndex(1), ms, tx2Bytes, tx2)
	requireGigaFallbackTestSuccess(t, result2, 1)

	result3 := executeGigaFallbackTestTx(t, sei, blockCtx.WithTxIndex(2), tx3, cache)
	blockCtx.GigaMultiStore().WriteGiga()

	finalBalance := sei.EvmKeeper.GetBalance(ctx, sender.AccountAddress)
	expected := expectedGigaFallbackTestBalance(preBalance, values, gasPrice, result1, result2, result3)
	require.Equal(t, 0, finalBalance.Cmp(expected),
		"sender balance should include Giga tx1, V2 fallback tx2, and Giga tx3")
}

func TestProcessTxsSynchronousGigaFallsBackOnIteratorUnsupported(t *testing.T) {
	blockTime := time.Now()
	validator := newGigaFallbackTestSigner(t)
	sender := newGigaFallbackTestSigner(t)
	recipient1 := newGigaFallbackTestSigner(t)
	recipient2 := newGigaFallbackTestSigner(t)
	recipient3 := newGigaFallbackTestSigner(t)

	wrapper := NewGigaTestWrapper(t, blockTime, validator.PublicKey, true, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(false)
		ba.SetConcurrencyWorkers(1)
	})
	sei := wrapper.App
	ctx := wrapper.Ctx.WithBlockHeader(tmproto.Header{
		Height:  wrapper.Ctx.BlockHeader().Height,
		ChainID: wrapper.Ctx.BlockHeader().ChainID,
		Time:    blockTime,
	})

	initialFundingWei := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000_000))
	gasPrice := big.NewInt(100_000_000_000)
	values := []*big.Int{
		big.NewInt(1_000_000_000_000),
		big.NewInt(2_000_000_000_000),
		big.NewInt(3_000_000_000_000),
	}

	sei.EvmKeeper.SetAddressMapping(ctx, sender.AccountAddress, sender.EvmAddress)
	fundWeiForGigaFallbackTest(t, sei, ctx, sender.AccountAddress, initialFundingWei)

	sei.GigaEvmKeeper.UseRegularStore = false
	sei.GigaBankKeeper.UseRegularStore = false

	preBalance := sei.EvmKeeper.GetBalance(ctx, sender.AccountAddress)
	tx1Bytes, tx1 := buildGigaFallbackTestEVMTx(t, sender, &recipient1.EvmAddress, values[0], nil, 21_000, gasPrice, 0)
	tx2Bytes, tx2 := buildGigaFallbackTestEVMTx(t, sender, &recipient2.EvmAddress, values[1], nil, 21_000, gasPrice, 1)
	tx3Bytes, tx3 := buildGigaFallbackTestEVMTx(t, sender, &recipient3.EvmAddress, values[2], nil, 21_000, gasPrice, 2)

	hookCalls := 0
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), executeEVMTxWithGigaExecutorAfterValidationHookKey{}, func(execCtx sdk.Context) {
		if execCtx.TxIndex() != 1 {
			return
		}
		hookCalls++
		iter := execCtx.GigaKVStore(sei.GetKey(evmtypes.StoreKey)).Iterator(nil, nil)
		require.NoError(t, iter.Close())
	}))

	results := sei.ProcessTxsSynchronousGiga(ctx, [][]byte{tx1Bytes, tx2Bytes, tx3Bytes}, []sdk.Tx{tx1, tx2, tx3})
	require.Len(t, results, 3)
	require.Equal(t, 1, hookCalls)
	for i, result := range results {
		requireGigaFallbackTestSuccess(t, result, i)
	}

	finalBalance := sei.EvmKeeper.GetBalance(ctx, sender.AccountAddress)
	expected := expectedGigaFallbackTestBalance(preBalance, values, gasPrice, results...)
	require.Equal(t, 0, finalBalance.Cmp(expected),
		"sender balance should include Giga tx1, ErrIteratorUnsupported fallback tx2, and Giga tx3")
}

func buildGigaFallbackTestEVMTx(
	t testing.TB,
	signer gigaFallbackTestAcct,
	to *common.Address,
	value *big.Int,
	data []byte,
	gasLimit uint64,
	gasPrice *big.Int,
	nonce uint64,
) ([]byte, sdk.Tx) {
	t.Helper()

	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		GasFeeCap: gasPrice,
		GasTipCap: gasPrice,
		Gas:       gasLimit,
		ChainID:   big.NewInt(config.DefaultChainID),
		To:        to,
		Value:     value,
		Data:      data,
		Nonce:     nonce,
	}), signer.EvmSigner, signer.EvmPrivateKey)
	require.NoError(t, err)

	txData, err := ethtx.NewTxDataFromTx(signedTx)
	require.NoError(t, err)

	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)

	txBuilder := MakeEncodingConfig().TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBuilder.SetGasLimit(10_000_000_000)

	tx := txBuilder.GetTx()
	txBytes, err := MakeEncodingConfig().TxConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return txBytes, tx
}

func executeGigaFallbackTestTx(
	t testing.TB,
	sei *App,
	ctx sdk.Context,
	tx sdk.Tx,
	cache *gigaBlockCache,
) *abci.ExecTxResult {
	t.Helper()

	msg := sei.GetEVMMsg(tx)
	require.NotNil(t, msg)
	result, err := sei.executeEVMTxWithGigaExecutor(ctx, msg, cache)
	require.NoError(t, err)
	requireGigaFallbackTestSuccess(t, result, ctx.TxIndex())
	return result
}

func requireGigaFallbackTestSuccess(t testing.TB, result *abci.ExecTxResult, txIndex int) {
	t.Helper()

	require.NotNil(t, result)
	require.Equal(t, uint32(0), result.Code, "tx[%d] should succeed: %s", txIndex, result.Log)
	require.NotNil(t, result.EvmTxInfo, "tx[%d] should include EVM info", txIndex)
	require.Equal(t, uint64(txIndex), result.EvmTxInfo.Nonce, "tx[%d] nonce", txIndex)
}

func expectedGigaFallbackTestBalance(
	preBalance *big.Int,
	values []*big.Int,
	gasPrice *big.Int,
	results ...*abci.ExecTxResult,
) *big.Int {
	loss := new(big.Int)
	for _, value := range values {
		loss.Add(loss, value)
	}
	for _, result := range results {
		loss.Add(loss, new(big.Int).Mul(big.NewInt(result.GasUsed), gasPrice))
	}
	return new(big.Int).Sub(preBalance, loss)
}

func fundWeiForGigaFallbackTest(t testing.TB, sei *App, ctx sdk.Context, addr sdk.AccAddress, amountWei *big.Int) {
	t.Helper()

	usei := new(big.Int).Div(new(big.Int).Set(amountWei), big.NewInt(1_000_000_000_000))
	coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewIntFromBigInt(usei)))
	require.NoError(t, sei.BankKeeper.MintCoins(ctx, "mint", coins))
	require.NoError(t, sei.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", addr, coins))
}

func newGigaFallbackTestSigner(t testing.TB) gigaFallbackTestAcct {
	t.Helper()

	priv, pubKey, acct := testdata.KeyTestPubAddr()
	key, err := crypto.HexToECDSA(hex.EncodeToString(priv.Bytes()))
	require.NoError(t, err)

	ethCfg := evmtypes.DefaultChainConfig().EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), 1)

	return gigaFallbackTestAcct{
		AccountAddress: acct,
		PublicKey:      pubKey,
		EvmAddress:     crypto.PubkeyToAddress(key.PublicKey),
		EvmSigner:      signer,
		EvmPrivateKey:  key,
	}
}
