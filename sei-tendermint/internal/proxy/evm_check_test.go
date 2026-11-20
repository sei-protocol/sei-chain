package proxy

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type evmCheckApp struct {
	EVMApplication
	check     func(EvmTx) (AdmittedTx, error)
	finalized []EvmTx
}

func (a *evmCheckApp) CheckTx(tx EvmTx) (AdmittedTx, error) { return a.check(tx) }

func TestEVMCheckTx(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		want := AdmittedTx{GasEstimated: 21_000}
		raw := signedProxyTestTx(0)
		app := NewEVM(&evmCheckApp{check: func(tx EvmTx) (AdmittedTx, error) {
			require.Equal(t, raw.Hash(), tx.Hash())
			return want, nil
		}})
		decoded := utils.OrPanic1(app.DecodeTx(utils.OrPanic1(raw.MarshalBinary())))
		got, err := app.CheckTx(decoded)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("rejected", func(t *testing.T) {
		cause := errors.New("invalid transaction")
		app := NewEVM(&evmCheckApp{check: func(EvmTx) (AdmittedTx, error) { return AdmittedTx{}, cause }})
		_, err := app.CheckTx(utils.OrPanic1(app.DecodeTx(utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()))))
		require.ErrorIs(t, err, ErrTxRejected)
		require.ErrorIs(t, err, cause)
	})
	t.Run("panic propagates", func(t *testing.T) {
		app := NewEVM(&evmCheckApp{check: func(EvmTx) (AdmittedTx, error) { panic("store failure") }})
		require.Panics(t, func() {
			_, _ = app.CheckTx(utils.OrPanic1(app.DecodeTx(utils.OrPanic1(signedProxyTestTx(0).MarshalBinary()))))
		})
	})
}

func TestDecodeTxRejectsMalformedInput(t *testing.T) {
	_, err := NewEVM(&evmCheckApp{}).DecodeTx([]byte{1})
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
	proxy := NewEVM(app)
	decoded, err := proxy.DecodeTx(raw)
	require.NoError(t, err)
	require.Equal(t, proxyTestSender(tx), decoded.Sender())
	_, err = proxy.CheckTx(decoded)
	require.NoError(t, err)
	for senders := range proxy.checkedSenders.Lock() {
		require.Equal(t, utils.Some(proxyTestSender(tx)), senders.peek(tx.Hash()))
	}
	decodedBlock, err := proxy.DecodeTxs([][]byte{raw}, 1)
	require.NoError(t, err)
	_, err = proxy.FinalizeBlock(t.Context(), FinalizeBlockReq{Txs: decodedBlock})
	require.NoError(t, err)
	require.Equal(t, proxyTestSender(tx), app.finalized[0].Sender())
	for senders := range proxy.checkedSenders.Lock() {
		require.False(t, senders.peek(tx.Hash()).IsPresent())
	}
	proxy.RememberEvmSender(tx.Hash(), proxyTestSender(tx))
	proxy.ForgetEvmSender(tx.Hash())
	for senders := range proxy.checkedSenders.Lock() {
		require.False(t, senders.peek(tx.Hash()).IsPresent())
	}
}

func TestEVMProxyDoesNotCacheRejectedAdmission(t *testing.T) {
	tx := signedProxyTestTx(0)
	app := &evmCheckApp{check: func(EvmTx) (AdmittedTx, error) {
		return AdmittedTx{}, errors.New("invalid transaction")
	}}
	proxy := NewEVM(app)
	decoded := utils.OrPanic1(proxy.DecodeTx(utils.OrPanic1(tx.MarshalBinary())))
	_, err := proxy.CheckTx(decoded)
	require.ErrorIs(t, err, ErrTxRejected)
	for senders := range proxy.checkedSenders.Lock() {
		require.False(t, senders.peek(tx.Hash()).IsPresent())
	}
}

func TestDecodeTxRejectsInvalidSignature(t *testing.T) {
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.LegacyTx{}).MarshalBinary())
	_, err := NewEVM(&evmCheckApp{}).DecodeTx(raw)
	require.ErrorIs(t, err, ErrTxRejected)
}

func TestDecodeTxRejectsZeroChainID(t *testing.T) {
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(0)}).MarshalBinary())
	_, err := NewEVM(&evmCheckApp{}).DecodeTx(raw)
	require.ErrorIs(t, err, ErrTxRejected)
	require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
}

func TestDecodeTxRecoversUnprotectedSender(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{}), ethtypes.HomesteadSigner{}, key)
	require.NoError(t, err)
	tx, err := NewEVM(&evmCheckApp{}).DecodeTx(utils.OrPanic1(signed.MarshalBinary()))
	require.NoError(t, err)
	require.Equal(t, crypto.PubkeyToAddress(key.PublicKey), tx.Sender())
}

func TestDecodeTxsPreservesOrder(t *testing.T) {
	raw := make([][]byte, 128)
	for i := range raw {
		raw[i] = utils.OrPanic1(signedProxyTestTx(uint64(i)).MarshalBinary())
	}
	for _, workers := range []int{0, 1, 4} {
		txs, err := NewEVM(&evmCheckApp{}).DecodeTxs(raw, workers)
		require.NoError(t, err)
		require.Equal(t, len(raw), len(txs))
		for i, tx := range txs {
			require.Equal(t, uint64(i), tx.Nonce())
			require.Equal(t, proxyTestSender(signedProxyTestTx(uint64(i))), tx.Sender())
		}
	}
	raw[17] = []byte{1}
	_, err := NewEVM(&evmCheckApp{}).DecodeTxs(raw, 0)
	require.ErrorIs(t, err, ErrTxRejected)
}
