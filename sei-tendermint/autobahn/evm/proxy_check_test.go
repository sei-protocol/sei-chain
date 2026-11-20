package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type evmCheckApp struct {
	Application
	check     func(EvmTx) (AdmittedTx, error)
	finalized []EvmTx
}

func (a *evmCheckApp) CheckTx(tx EvmTx) (AdmittedTx, error) { return a.check(tx) }

func TestEVMCheckTx(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		want := AdmittedTx{GasEstimated: 21_000}
		raw := signedProxyTestTx(0)
		app := NewProxy(&evmCheckApp{check: func(tx EvmTx) (AdmittedTx, error) {
			require.Equal(t, raw.Hash(), tx.Hash())
			return want, nil
		}})
		decoded := utils.OrPanic1(app.ParseTx(1, utils.OrPanic1(raw.MarshalBinary())))
		got, err := app.CheckTx(decoded)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("rejected", func(t *testing.T) {
		cause := errors.New("invalid transaction")
		app := NewProxy(&evmCheckApp{check: func(EvmTx) (AdmittedTx, error) { return AdmittedTx{}, cause }})
		_, err := app.CheckTx(utils.OrPanic1(app.ParseTx(1, utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()))))
		require.ErrorIs(t, err, ErrTxRejected)
		require.ErrorIs(t, err, cause)
	})
	t.Run("panic propagates", func(t *testing.T) {
		app := NewProxy(&evmCheckApp{check: func(EvmTx) (AdmittedTx, error) { panic("store failure") }})
		require.Panics(t, func() {
			_, _ = app.CheckTx(utils.OrPanic1(app.ParseTx(1, utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()))))
		})
	})
}

func TestParseTxRejectsMalformedInput(t *testing.T) {
	_, err := NewProxy(&evmCheckApp{}).ParseTx(1, []byte{1})
	require.ErrorIs(t, err, ErrTxRejected)
}

func (a *evmCheckApp) FinalizeBlock(_ context.Context, req FinalizeBlockReq) (FinalizeBlockResp, error) {
	a.finalized = req.Txs
	return FinalizeBlockResp{}, nil
}

func signedProxyTestTx(nonce uint64) *ethtypes.Transaction {
	key := utils.OrPanic1(crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe512961708279011d44f4d6b2c7d7f7"))
	tx := utils.OrPanic1(ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: nonce}), ethtypes.LatestSignerForChainID(big.NewInt(1)), key))
	return tx
}

func proxyTestSender(tx *ethtypes.Transaction) common.Address {
	return utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx))
}

func TestEVMProxySenderCacheLifecycle(t *testing.T) {
	tx := signedProxyTestTx(0)
	raw := utils.OrPanic1(tx.MarshalBinary())
	app := &evmCheckApp{check: func(got EvmTx) (AdmittedTx, error) {
		require.Equal(t, proxyTestSender(tx), got.Sender())
		return AdmittedTx{}, nil
	}}
	proxy := NewProxy(app)
	decoded, err := proxy.ParseTx(1, raw)
	require.NoError(t, err)
	require.Equal(t, proxyTestSender(tx), decoded.Sender())
	_, err = proxy.CheckTx(decoded)
	require.NoError(t, err)
	for senders := range proxy.checkedSenders.Lock() {
		require.Equal(t, utils.Some(proxyTestSender(tx)), senders.get(tx.Hash()))
	}
	decodedBlock, err := proxy.ParseTxs(1, [][]byte{raw}, 1)
	require.NoError(t, err)
	_, err = proxy.FinalizeBlock(t.Context(), FinalizeBlockReq{Txs: decodedBlock})
	require.NoError(t, err)
	require.Equal(t, proxyTestSender(tx), app.finalized[0].Sender())
	for senders := range proxy.checkedSenders.Lock() {
		require.False(t, senders.get(tx.Hash()).IsPresent())
	}
	proxy.RememberEvmSender(tx.Hash(), proxyTestSender(tx))
	proxy.ForgetEvmSender(tx.Hash())
	for senders := range proxy.checkedSenders.Lock() {
		require.False(t, senders.get(tx.Hash()).IsPresent())
	}
}

func TestEVMProxyDoesNotCacheRejectedAdmission(t *testing.T) {
	tx := signedProxyTestTx(0)
	app := &evmCheckApp{check: func(EvmTx) (AdmittedTx, error) {
		return AdmittedTx{}, errors.New("invalid transaction")
	}}
	proxy := NewProxy(app)
	decoded := utils.OrPanic1(proxy.ParseTx(1, utils.OrPanic1(tx.MarshalBinary())))
	_, err := proxy.CheckTx(decoded)
	require.ErrorIs(t, err, ErrTxRejected)
	for senders := range proxy.checkedSenders.Lock() {
		require.False(t, senders.get(tx.Hash()).IsPresent())
	}
}

func TestParseTxRejectsInvalidSignature(t *testing.T) {
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(1)}).MarshalBinary())
	_, err := NewProxy(&evmCheckApp{}).ParseTx(1, raw)
	require.ErrorIs(t, err, ErrTxRejected)
	require.ErrorIs(t, err, ethtypes.ErrInvalidSig)
}

func TestParseTxRejectsZeroChainID(t *testing.T) {
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(0)}).MarshalBinary())
	_, err := NewProxy(&evmCheckApp{}).ParseTx(1, raw)
	require.ErrorIs(t, err, ErrTxRejected)
	require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
}

func TestParseTxChecksChainBeforeSender(t *testing.T) {
	app := NewProxy(&evmCheckApp{})
	tx := signedProxyTestTx(0)
	raw := utils.OrPanic1(tx.MarshalBinary())
	app.RememberEvmSender(tx.Hash(), proxyTestSender(tx))
	for _, chainID := range []uint64{0, 2} {
		_, err := app.ParseTx(chainID, raw)
		require.ErrorIs(t, err, ErrTxRejected)
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
		for _, workers := range []int{1, 4} {
			_, err := app.ParseTxs(chainID, [][]byte{raw, raw}, workers)
			require.ErrorIs(t, err, ErrTxRejected)
			require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
		}
	}
	oversized := new(big.Int).Lsh(big.NewInt(1), 64)
	oversized.Add(oversized, big.NewInt(1))
	for _, wrongChain := range []*big.Int{big.NewInt(2), oversized} {
		invalidSignature := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: wrongChain}).MarshalBinary())
		_, err := app.ParseTx(1, invalidSignature)
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
		_, err = app.ParseTxs(1, [][]byte{raw, invalidSignature}, 4)
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	}
	accepted, err := app.ParseTx(1, raw)
	require.NoError(t, err)
	require.Equal(t, proxyTestSender(tx), accepted.Sender())
}

func TestParseTxRejectsUnprotectedTransaction(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{}), ethtypes.HomesteadSigner{}, key)
	require.NoError(t, err)
	app := NewProxy(&evmCheckApp{})
	raw := utils.OrPanic1(signed.MarshalBinary())
	_, err = app.ParseTx(1, raw)
	require.ErrorIs(t, err, ErrTxRejected)
	app.RememberEvmSender(signed.Hash(), crypto.PubkeyToAddress(key.PublicKey))
	_, err = app.ParseTx(1, raw)
	require.ErrorIs(t, err, ErrTxRejected)
	for _, workers := range []int{1, 4} {
		_, err = app.ParseTxs(1, [][]byte{utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()), raw}, workers)
		require.ErrorIs(t, err, ErrTxRejected)
	}
}

func TestParseTxsPreservesOrder(t *testing.T) {
	raw := make([][]byte, 2*minTxsPerHashWorker+1)
	app := NewProxy(&evmCheckApp{})
	sender := proxyTestSender(signedProxyTestTx(0))
	for i := range raw {
		tx := signedProxyTestTx(uint64(i))
		raw[i] = utils.OrPanic1(tx.MarshalBinary())
		if i%2 == 0 {
			app.RememberEvmSender(tx.Hash(), sender)
		}
	}
	for _, count := range []int{0, 1, minTxsPerHashWorker - 1, minTxsPerHashWorker, minTxsPerHashWorker + 1, len(raw)} {
		for _, workers := range []int{0, 1, 4} {
			txs, err := app.ParseTxs(1, raw[:count], workers)
			require.NoError(t, err)
			require.Equal(t, count, len(txs))
			for i, tx := range txs {
				require.Equal(t, uint64(i), tx.Nonce())
				require.Equal(t, sender, tx.Sender())
				require.Equal(t, signedProxyTestTx(uint64(i)).Hash(), tx.Hash())
			}
		}
	}
	valid := raw[minTxsPerHashWorker]
	raw[minTxsPerHashWorker] = []byte{1}
	_, err := app.ParseTxs(1, raw, 4)
	require.ErrorIs(t, err, ErrTxRejected)
	raw[minTxsPerHashWorker] = valid
	raw[minTxsPerHashWorker+1] = utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(1)}).MarshalBinary())
	_, err = app.ParseTxs(1, raw, 4)
	require.ErrorIs(t, err, ErrTxRejected)
	require.ErrorIs(t, err, ethtypes.ErrInvalidSig)
}

func BenchmarkParseTxs(b *testing.B) {
	for _, count := range []int{64, 1024} {
		for _, cached := range []bool{false, true} {
			b.Run(fmt.Sprintf("txs=%d/cached=%t", count, cached), func(b *testing.B) {
				app := NewProxy(&evmCheckApp{})
				raw := make([][]byte, count)
				for i := range raw {
					tx := signedProxyTestTx(uint64(i))
					raw[i] = utils.OrPanic1(tx.MarshalBinary())
					if cached {
						app.RememberEvmSender(tx.Hash(), proxyTestSender(tx))
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := app.ParseTxs(1, raw, 4); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
