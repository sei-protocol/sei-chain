package evm

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type evmCheckApp struct {
	Application
	check func(EvmTx) (AdmittedTx, error)
}

func (a *evmCheckApp) EvmChainID() uint64 { return 1 }

func (a *evmCheckApp) CheckTx(tx EvmTx) (AdmittedTx, error) { return a.check(tx) }

func TestEVMCheckTx(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		want := AdmittedTx{GasEstimated: 21_000}
		raw := signedProxyTestTx(0)
		app := NewProxy(&evmCheckApp{check: func(tx EvmTx) (AdmittedTx, error) {
			require.Equal(t, raw.Hash(), tx.Hash())
			return want, nil
		}})
		decoded := utils.OrPanic1(newTestSigner(1, 0).ParseTx(utils.OrPanic1(raw.MarshalBinary())))
		got, err := app.CheckTx(decoded.EvmTx())
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("rejected", func(t *testing.T) {
		cause := errors.New("invalid transaction")
		app := NewProxy(&evmCheckApp{check: func(EvmTx) (AdmittedTx, error) { return AdmittedTx{}, cause }})
		_, err := app.CheckTx(utils.OrPanic1(newTestSigner(1, 0).ParseTx(utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()))).EvmTx())
		require.ErrorIs(t, err, ErrTxRejected)
		require.ErrorIs(t, err, cause)
	})
	t.Run("panic propagates", func(t *testing.T) {
		app := NewProxy(&evmCheckApp{check: func(EvmTx) (AdmittedTx, error) { panic("store failure") }})
		require.Panics(t, func() {
			_, _ = app.CheckTx(utils.OrPanic1(newTestSigner(1, 0).ParseTx(utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()))).EvmTx())
		})
	})
}

func TestParseTxRejectsMalformedInput(t *testing.T) {
	_, err := newTestSigner(1, 0).ParseTx([]byte{1})
	require.Error(t, err)
}

func signedProxyTestTx(nonce uint64) *ethtypes.Transaction {
	key := utils.OrPanic1(crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe512961708279011d44f4d6b2c7d7f7"))
	tx := utils.OrPanic1(ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: nonce}), ethtypes.LatestSignerForChainID(big.NewInt(1)), key))
	return tx
}

func proxyTestSender(tx *ethtypes.Transaction) common.Address {
	return utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx))
}

func TestParseTxRejectsBlobTransactions(t *testing.T) {
	// The unsigned transaction also has an invalid signature, so the error proves
	// rejection happens before sender recovery.
	tx := ethtypes.NewTx(&ethtypes.BlobTx{ChainID: uint256.NewInt(1)})
	raw := utils.OrPanic1(tx.MarshalBinary())
	_, err := newTestSigner(1, 0).ParseTx(raw)
	require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
	for _, workers := range []int{1, 4} {
		_, err := newTestSigner(1, workers).ParseTxs([]RawTx{raw})
		require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
	}
}

func TestParseTxRejectsInvalidSignature(t *testing.T) {
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(1)}).MarshalBinary())
	_, err := newTestSigner(1, 0).ParseTx(raw)
	require.Error(t, err)
	require.ErrorIs(t, err, ethtypes.ErrInvalidSig)
}

func TestParseTxRejectsMismatchedZeroChainID(t *testing.T) {
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(0)}).MarshalBinary())
	_, err := newTestSigner(1, 0).ParseTx(raw)
	require.Error(t, err)
	require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
}

func TestParseTxChecksChainBeforeSender(t *testing.T) {
	recoverer := &TestCountingSigner{Signer: ethtypes.LatestSignerForChainID(big.NewInt(1))}
	app := NewSigner(recoverer, 1024, 4)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx := signedProxyTestTx(0)
	raw := utils.OrPanic1(tx.MarshalBinary())
	sender := proxyTestSender(tx)
	app.Remember(TestSignedEvmTx(tx))
	oversized := new(big.Int).Lsh(big.NewInt(1), 64)
	oversized.Add(oversized, big.NewInt(1))
	for _, wrongChain := range []*big.Int{big.NewInt(2), oversized} {
		invalid, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: wrongChain}), ethtypes.LatestSignerForChainID(wrongChain), key)
		require.NoError(t, err)
		invalidRaw := utils.OrPanic1(invalid.MarshalBinary())
		app.Remember(TestSignedEvmTx(invalid))
		_, err = app.ParseTx(invalidRaw)
		require.Error(t, err)
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
		for _, workers := range []int{1, 4} {
			batch := newTestSigner(1, workers)
			batch.Remember(TestSignedEvmTx(invalid))
			_, err := batch.ParseTxs([]RawTx{raw, invalidRaw})
			require.Error(t, err)
			require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
		}
	}
	require.Equal(t, uint64(0), recoverer.Calls.Load())
	accepted, err := app.ParseTx(raw)
	require.NoError(t, err)
	require.Equal(t, sender, accepted.Sender())
}

func TestParseTxRejectsUnprotectedTransaction(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{}), ethtypes.HomesteadSigner{}, key)
	require.NoError(t, err)
	app := newTestSigner(1, 0)
	raw := utils.OrPanic1(signed.MarshalBinary())
	_, err = app.ParseTx(raw)
	require.Error(t, err)
	for _, workers := range []int{1, 4} {
		batch := newTestSigner(1, workers)
		_, err = batch.ParseTxs([]RawTx{utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()), raw})
		require.Error(t, err)
	}
}

func TestParseTxsPreservesOrder(t *testing.T) {
	raw := make([]RawTx, 2*minTxsPerHashWorker+1)
	transactions := make([]*ethtypes.Transaction, len(raw))
	sender := proxyTestSender(signedProxyTestTx(0))
	for i := range raw {
		transactions[i] = signedProxyTestTx(uint64(i))
		raw[i] = utils.OrPanic1(transactions[i].MarshalBinary())
	}
	for _, workers := range []int{0, 1, 4} {
		app := newTestSigner(1, workers)
		for i := range raw {
			if i%2 == 0 {
				app.Remember(TestSignedEvmTx(transactions[i]))
			}
		}
		for _, count := range []int{0, 1, minTxsPerHashWorker - 1, minTxsPerHashWorker, minTxsPerHashWorker + 1, len(raw)} {
			txs, err := app.ParseTxs(raw[:count])
			require.NoError(t, err)
			require.Equal(t, count, len(txs))
			for i, tx := range txs {
				require.Equal(t, uint64(i), tx.Nonce())
				require.Equal(t, sender, tx.Sender())
				require.Equal(t, signedProxyTestTx(uint64(i)).Hash(), tx.Hash())
			}
		}
	}
	app := newTestSigner(1, 4)
	valid := raw[minTxsPerHashWorker]
	raw[minTxsPerHashWorker] = []byte{1}
	_, err := app.ParseTxs(raw)
	require.Error(t, err)
	raw[minTxsPerHashWorker] = valid
	raw[minTxsPerHashWorker+1] = utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(1)}).MarshalBinary())
	_, err = app.ParseTxs(raw)
	require.Error(t, err)
	require.ErrorIs(t, err, ethtypes.ErrInvalidSig)
}

func BenchmarkParseTxs(b *testing.B) {
	for _, count := range []int{64, 1024} {
		for _, cached := range []bool{false, true} {
			b.Run(fmt.Sprintf("txs=%d/cached=%t", count, cached), func(b *testing.B) {
				app := newTestSigner(1, 4)
				raw := make([]RawTx, count)
				for i := range raw {
					tx := signedProxyTestTx(uint64(i))
					raw[i] = utils.OrPanic1(tx.MarshalBinary())
					if cached {
						app.Remember(TestSignedEvmTx(tx))
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := app.ParseTxs(raw); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func newTestSigner(chainID uint64, workers int) *Signer {
	return NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}
