package evm

import (
	"crypto/ecdsa"
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
	claimed := testAddress(0xc2)

	t.Run("known sender is used without recovery", func(t *testing.T) {
		prepared, err := parsePreparedTx(rawTx, signer, utils.Some(claimed))
		require.NoError(t, err)
		require.Equal(t, claimed, prepared.Sender)
		require.Equal(t, tx.Hash(), prepared.Tx.Hash())
	})

	t.Run("no known sender recovers", func(t *testing.T) {
		prepared, err := parsePreparedTx(rawTx, signer, utils.None[common.Address]())
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender)
	})

	t.Run("known sender is ignored for a tx from another chain", func(t *testing.T) {
		otherChain := big.NewInt(testChainID + 1)
		otherRaw := signLegacyTx(t, key, otherChain, 0, &recipient, big.NewInt(1), nil)
		_, err := parsePreparedTx(otherRaw, signer, utils.Some(claimed))
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	})

	t.Run("known sender is ignored for a type the signer does not support", func(t *testing.T) {
		legacyOnly := ethtypes.NewEIP155Signer(chainID)
		dynamicRaw := signDynamicFeeTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		_, err := parsePreparedTx(dynamicRaw, legacyOnly, utils.Some(claimed))
		require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
	})

	t.Run("known sender is ignored for a signer without a chain ID", func(t *testing.T) {
		_, err := parsePreparedTx(rawTx, ethtypes.HomesteadSigner{}, utils.Some(claimed))
		require.ErrorIs(t, err, ethtypes.ErrInvalidSig)
	})
}

func TestParseBlockTxsMixesKnownAndRecoveredSenders(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	recipient := testAddress(0xc3)
	const n = 8
	raws := make([][]byte, n)
	senders := make([]common.Address, n)
	known := make([]utils.Option[common.Address], n)
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		if i%2 == 0 {
			known[i] = utils.Some(senders[i])
		}
	}
	for _, workers := range []int{1, 4} {
		parsed, err := parseBlockTxs(raws, signer, known, workers)
		require.NoError(t, err)
		require.Len(t, parsed, n)
		for i, prepared := range parsed {
			require.Equal(t, senders[i], prepared.Sender)
		}
	}
}

func TestParseBlockTxsProcessesMultipleClaims(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	recipient := testAddress(0xc8)
	const n = 40
	raws := make([][]byte, n)
	senders := make([]common.Address, n)
	known := make([]utils.Option[common.Address], n)
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, uint64(i), &recipient, big.NewInt(1), nil)
		if i%2 == 0 {
			known[i] = utils.Some(senders[i])
		}
	}

	parsed, err := parseBlockTxs(raws, signer, known, 4)
	require.NoError(t, err)
	require.Len(t, parsed, n)
	for i, prepared := range parsed {
		require.Equal(t, senders[i], prepared.Sender)
		require.Equal(t, decodeTx(t, raws[i]).Hash(), prepared.Tx.Hash())
	}
}

func BenchmarkParsePreparedTx(b *testing.B) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	key, err := crypto.GenerateKey()
	require.NoError(b, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xc6)
	rawTx := signLegacyTx(b, key, chainID, 0, &recipient, big.NewInt(1), nil)
	known := utils.Some(sender)

	b.Run("recover", func(b *testing.B) {
		for b.Loop() {
			if _, err := parsePreparedTx(rawTx, signer, utils.None[common.Address]()); err != nil {
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

type parsedTestTx struct {
	Tx     *ethtypes.Transaction
	Sender common.Address
}

func parseBlockTxs(raw [][]byte, signer ethtypes.Signer, known []utils.Option[common.Address], workers int) ([]parsedTestTx, error) {
	txs, err := DecodeBlockTxs(raw, workers, func(*ethtypes.Transaction) error { return nil })
	if err != nil {
		return nil, err
	}
	senders, err := ResolveBlockSenders(txs, signer, known, workers)
	if err != nil {
		return nil, err
	}
	parsed := make([]parsedTestTx, len(txs))
	for i, tx := range txs {
		parsed[i] = parsedTestTx{Tx: tx, Sender: senders[i]}
	}
	return parsed, nil
}

func parsePreparedTx(raw []byte, signer ethtypes.Signer, known utils.Option[common.Address]) (parsedTestTx, error) {
	txs, err := parseBlockTxs([][]byte{raw}, signer, []utils.Option[common.Address]{known}, 1)
	if err != nil {
		return parsedTestTx{}, err
	}
	return txs[0], nil
}

func testAddress(suffix byte) common.Address { return common.BytesToAddress([]byte{suffix}) }

func decodeTx(t *testing.T, raw []byte) *ethtypes.Transaction {
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
