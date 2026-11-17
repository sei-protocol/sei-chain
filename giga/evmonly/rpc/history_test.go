package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/archive"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var _ History = (*archive.Reader)(nil)

// fakeHistory is a History that returns tx or err and counts its calls.
type fakeHistory struct {
	tx    archive.Transaction
	err   error
	calls int
}

func (h *fakeHistory) TransactionByHash(context.Context, common.Hash) (archive.Transaction, error) {
	h.calls++
	return h.tx, h.err
}

var (
	historyTestChainID   = big.NewInt(713700)
	historyTestBlockHash = common.HexToHash("0xabcd")
	historyTestBlockTime = time.Unix(1_700_000_000, 0)
)

// signedHistoryTestTx returns a signed dynamic-fee transaction and its sender.
func signedHistoryTestTx(t *testing.T) (*ethtypes.Transaction, common.Address) {
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(unsignedHistoryTestTx(), ethtypes.LatestSignerForChainID(historyTestChainID), key)
	require.NoError(t, err)
	return tx, crypto.PubkeyToAddress(key.PublicKey)
}

// unsignedHistoryTestTx returns a dynamic-fee transaction whose sender does not recover.
func unsignedHistoryTestTx() *ethtypes.Transaction {
	to := common.HexToAddress("0x2000000000000000000000000000000000000002")
	return ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   historyTestChainID,
		Nonce:     5,
		GasTipCap: big.NewInt(1_500_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       50_000,
		To:        &to,
		Value:     big.NewInt(7),
		Data:      []byte{0xde, 0xad, 0xbe, 0xef},
	})
}

// liveTxAPI returns a txAPI whose own stores hold tx, sent by sender, at index 1 of block 9.
func liveTxAPI(t *testing.T, tx *ethtypes.Transaction, sender common.Address) *txAPI {
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(),
		Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      9,
			TransactionIndex: 1,
			From:             sender.Hex(),
		},
	}}))
	return &txAPI{backend: historyTestBackend(t, raw), store: store}
}

// historyTestBackend returns a backend whose block 9 holds raw at index 1, or no block when raw is
// nil.
func historyTestBackend(t *testing.T, raw []byte) *testBackend {
	chainConfig := testChainConfig(historyTestChainID)
	return &testBackend{
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			if raw == nil {
				t.Fatal("block lookup reached for a hash the node does not hold")
			}
			require.Equal(t, coretypes.Int64(9), *req.Height)
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: historyTestBlockHash.Bytes()},
				Block: &tmtypes.Block{
					Header: tmtypes.Header{Time: historyTestBlockTime},
					Data:   tmtypes.Data{Txs: tmtypes.Txs{[]byte("some other transaction"), raw}},
				},
			}, nil
		},
		chainConfig: func() *params.ChainConfig { return chainConfig },
		baseFee:     func() *big.Int { return new(big.Int) },
	}
}

// archivedTx returns tx, sent by sender, as the archive holds it at index 1 of block 9.
func archivedTx(tx *ethtypes.Transaction, sender common.Address) archive.Transaction {
	return archive.Transaction{
		Tx:     tx,
		Sender: sender,
		Index:  1,
		Block: archive.BlockHeader{
			Number:  9,
			Hash:    historyTestBlockHash,
			Time:    uint64(historyTestBlockTime.Unix()),
			BaseFee: new(big.Int),
		},
	}
}

// historyOnlyTxAPI returns a txAPI whose own stores are empty and whose history is history.
func historyOnlyTxAPI(t *testing.T, history *fakeHistory) *txAPI {
	return &txAPI{
		backend: historyTestBackend(t, nil),
		store:   evmonly.NewMemoryReceiptStore(),
		history: utils.Some[History](history),
	}
}

func TestGetTransactionByHashPrefersTheNodesStores(t *testing.T) {
	tx, sender := signedHistoryTestTx(t)
	api := liveTxAPI(t, tx, sender)
	history := &fakeHistory{err: errors.New("history must not be read")}
	api.history = utils.Some[History](history)

	got, err := api.GetTransactionByHash(t.Context(), tx.Hash())

	require.NoError(t, err)
	require.Equal(t, tx.Hash(), got.Hash)
	require.Zero(t, history.calls)
}

func TestGetTransactionByHashReadsHistoryOnAMiss(t *testing.T) {
	tx, sender := signedHistoryTestTx(t)
	history := &fakeHistory{tx: archivedTx(tx, sender)}

	got, err := historyOnlyTxAPI(t, history).GetTransactionByHash(t.Context(), tx.Hash())

	require.NoError(t, err)
	require.Equal(t, 1, history.calls)
	require.Equal(t, tx.Hash(), got.Hash)
	require.Equal(t, sender, got.From)
	require.Equal(t, historyTestBlockHash, *got.BlockHash)
	require.Equal(t, big.NewInt(9), got.BlockNumber.ToInt())
}

func TestGetTransactionByHashReturnsNullWhenHistoryHoldsNone(t *testing.T) {
	history := &fakeHistory{err: archive.ErrNotFound}

	got, err := historyOnlyTxAPI(t, history).GetTransactionByHash(t.Context(), common.Hash{9})

	require.NoError(t, err)
	require.Nil(t, got)
	require.Equal(t, 1, history.calls)
}

func TestGetTransactionByHashFailsWhenHistoryFails(t *testing.T) {
	history := &fakeHistory{err: errors.New("archive transaction_by_hash: connection refused")}

	got, err := historyOnlyTxAPI(t, history).GetTransactionByHash(t.Context(), common.Hash{9})

	require.ErrorIs(t, err, errHistoryUnavailable)
	require.Nil(t, got)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = historyOnlyTxAPI(t, history).GetTransactionByHash(ctx, common.Hash{9})
	require.ErrorIs(t, err, context.Canceled)
}

func TestGetTransactionByHashHistoryMatchesTheNodesStores(t *testing.T) {
	signed, signer := signedHistoryTestTx(t)
	unsigned := unsignedHistoryTestTx()
	storedSender := common.HexToAddress("0x1000000000000000000000000000000000000001")
	for name, tc := range map[string]struct {
		tx     *ethtypes.Transaction
		sender common.Address
	}{
		"signed": {signed, signer},
		// From comes from the stored sender on both paths.
		"sender does not recover": {unsigned, storedSender},
	} {
		t.Run(name, func(t *testing.T) {
			live, err := liveTxAPI(t, tc.tx, tc.sender).GetTransactionByHash(t.Context(), tc.tx.Hash())
			require.NoError(t, err)
			history := &fakeHistory{tx: archivedTx(tc.tx, tc.sender)}
			fromHistory, err := historyOnlyTxAPI(t, history).GetTransactionByHash(t.Context(), tc.tx.Hash())
			require.NoError(t, err)

			require.Equal(t, tc.sender, live.From)
			require.Equal(t, string(marshalJSON(t, live)), string(marshalJSON(t, fromHistory)))
		})
	}
}

func TestGetTransactionByHashReadsHistoryEndToEnd(t *testing.T) {
	tx, sender := signedHistoryTestTx(t)
	history := &fakeHistory{tx: archivedTx(tx, sender)}
	handler, err := newHandler(historyTestBackend(t, nil), evmonly.NewMemoryReceiptStore(), noRPCLimits, WithHistory(history))
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got *export.RPCTransaction
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getTransactionByHash", tx.Hash()))
	require.NotNil(t, got)
	require.Equal(t, tx.Hash(), got.Hash)

	history.err = errors.New("archive transaction_by_hash: connection refused")
	err = client.CallContext(t.Context(), &got, "eth_getTransactionByHash", tx.Hash())
	require.ErrorContains(t, err, errHistoryUnavailable.Error())
	require.NotContains(t, err.Error(), "connection refused")
}

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return out
}
