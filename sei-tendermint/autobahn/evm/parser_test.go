package evm

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestParsePreparedTxUsesKnownSender(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xc1)
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	tx := decodeTx(t, rawTx)
	knownTx := TestSignedEvmTx(tx)

	t.Run("known sender is used without recovery", func(t *testing.T) {
		recoverer := &TestCountingSigner{Signer: signer}
		prepared, err := parsePreparedTx(rawTx, recoverer, utils.Some(knownTx))
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender())
		require.Equal(t, tx.Hash(), prepared.Hash())
		require.Equal(t, uint64(0), recoverer.Calls.Load())
	})

	t.Run("no known sender recovers", func(t *testing.T) {
		prepared, err := parsePreparedTx(rawTx, signer, utils.None[EvmTx]())
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender())
	})

	t.Run("known sender is ignored for a tx from another chain", func(t *testing.T) {
		otherChain := big.NewInt(testChainID + 1)
		otherRaw := signLegacyTx(t, key, otherChain, 0, &recipient, big.NewInt(1), nil)
		_, err := parsePreparedTx(otherRaw, signer, utils.Some(TestSignedEvmTx(decodeTx(t, otherRaw))))
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	})

	t.Run("known sender is ignored for a type the signer does not support", func(t *testing.T) {
		legacyOnly := ethtypes.NewEIP155Signer(chainID)
		dynamicRaw := signDynamicFeeTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		_, err := parsePreparedTx(dynamicRaw, legacyOnly, utils.Some(TestSignedEvmTx(decodeTx(t, dynamicRaw))))
		require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
	})

	t.Run("known sender is ignored for a signer without a chain ID", func(t *testing.T) {
		_, err := parsePreparedTx(rawTx, ethtypes.HomesteadSigner{}, utils.Some(knownTx))
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	})
}

func TestParseBlockTxsMixesKnownAndRecoveredSenders(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	recipient := testAddress(0xc3)
	const n = 8
	raws := make([]RawTx, n)
	senders := make([]common.Address, n)
	known := make([]utils.Option[EvmTx], n)
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		if i%2 == 0 {
			known[i] = utils.Some(TestSignedEvmTx(decodeTx(t, raws[i])))
		}
	}
	for _, workers := range []int{1, 4} {
		parsed, err := parseBlockTxs(raws, signer, known, workers)
		require.NoError(t, err)
		require.Len(t, parsed, n)
		for i, prepared := range parsed {
			require.Equal(t, senders[i], prepared.Sender())
		}
	}
}

func TestParseBlockTxsProcessesMultipleClaims(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	recipient := testAddress(0xc8)
	const n = 40
	raws := make([]RawTx, n)
	senders := make([]common.Address, n)
	known := make([]utils.Option[EvmTx], n)
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, uint64(i), &recipient, big.NewInt(1), nil)
		if i%2 == 0 {
			known[i] = utils.Some(TestSignedEvmTx(decodeTx(t, raws[i])))
		}
	}

	parsed, err := parseBlockTxs(raws, signer, known, 4)
	require.NoError(t, err)
	require.Len(t, parsed, n)
	for i, prepared := range parsed {
		require.Equal(t, senders[i], prepared.Sender())
		require.Equal(t, decodeTx(t, raws[i]).Hash(), prepared.Hash())
	}
}

func BenchmarkParsePreparedTx(b *testing.B) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	key, err := crypto.GenerateKey()
	require.NoError(b, err)
	recipient := testAddress(0xc6)
	rawTx := signLegacyTx(b, key, chainID, 0, &recipient, big.NewInt(1), nil)
	known := utils.Some(TestSignedEvmTx(decodeTx(b, rawTx)))

	b.Run("recover", func(b *testing.B) {
		for b.Loop() {
			if _, err := parsePreparedTx(rawTx, signer, utils.None[EvmTx]()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("known", func(b *testing.B) {
		for b.Loop() {
			if _, err := parsePreparedTx(rawTx, signer, known); err != nil {
				b.Fatal(err)
			}
		}
	})
}

const testChainID = 713700

func parseBlockTxs(raw []RawTx, signer ethtypes.Signer, known []utils.Option[EvmTx], workers int) ([]EvmTx, error) {
	parser := NewSigner(signer, len(known), workers)
	for _, tx := range known {
		if tx, ok := tx.Get(); ok {
			parser.Remember(tx)
		}
	}
	return parser.ParseTxs(raw)
}

func parsePreparedTx(raw RawTx, signer ethtypes.Signer, known utils.Option[EvmTx]) (EvmTx, error) {
	txs, err := parseBlockTxs([]RawTx{raw}, signer, []utils.Option[EvmTx]{known}, 1)
	if err != nil {
		return EvmTx{}, err
	}
	return txs[0], nil
}

func testAddress(suffix byte) common.Address { return common.BytesToAddress([]byte{suffix}) }

func decodeTx(t testing.TB, raw []byte) *ethtypes.Transaction {
	t.Helper()
	tx := new(ethtypes.Transaction)
	require.NoError(t, tx.UnmarshalBinary(raw))
	return tx
}

func signLegacyTx(t testing.TB, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	return signParserTestTx(t, key, chainID, &ethtypes.LegacyTx{Nonce: nonce, To: to, Value: value, Data: data, Gas: 100_000, GasPrice: big.NewInt(1_000_000_000)})
}

func signDynamicFeeTx(t testing.TB, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	return signParserTestTx(t, key, chainID, &ethtypes.DynamicFeeTx{ChainID: chainID, Nonce: nonce, To: to, Value: value, Data: data, Gas: 100_000, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(1_000_000_000)})
}

func signParserTestTx(t testing.TB, key *ecdsa.PrivateKey, chainID *big.Int, data ethtypes.TxData) []byte {
	t.Helper()
	tx, err := ethtypes.SignTx(ethtypes.NewTx(data), ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func TestSignerOptionalCache(t *testing.T) {
	tx := signedProxyTestTx(0)
	raw := utils.OrPanic1(tx.MarshalBinary())
	sender := proxyTestSender(tx)
	for _, capacity := range []int{0, 1} {
		t.Run(fmt.Sprintf("capacity=%d", capacity), func(t *testing.T) {
			recoverer := &TestCountingSigner{Signer: ethtypes.LatestSignerForChainID(tx.ChainId())}
			signer := NewSigner(recoverer, capacity, 4)
			require.Equal(t, capacity > 0, signer.cache.IsPresent())
			signer.Remember(TestSignedEvmTx(tx))
			parsed, err := signer.ParseTx(raw)
			require.NoError(t, err)
			require.Equal(t, sender, parsed.Sender())
			require.Equal(t, RawTx(raw), parsed.Raw())
			batch, err := signer.ParseTxs([]RawTx{raw})
			require.NoError(t, err)
			require.Equal(t, sender, batch[0].Sender())
			wantCalls := uint64(0)
			if capacity == 0 {
				wantCalls = 2
			}
			require.Equal(t, wantCalls, recoverer.Calls.Load())
			signer.ForgetTxs(batch)
			parsed, err = signer.ParseTx(raw)
			require.NoError(t, err)
			require.Equal(t, sender, parsed.Sender())
			require.Equal(t, RawTx(raw), parsed.Raw())
			batch, err = signer.ParseTxs([]RawTx{raw})
			require.NoError(t, err)
			require.Equal(t, sender, batch[0].Sender())
			require.Equal(t, wantCalls+2, recoverer.Calls.Load())
		})
	}
}
