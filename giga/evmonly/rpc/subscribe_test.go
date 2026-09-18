package rpc

import (
	"context"
	"fmt"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// TestNewHeadsSubscriptionStreamsHeadersOverWebsocket delivers a head whose
// gasUsed comes from the commit itself: the receipt store is left empty.
func TestNewHeadsSubscriptionStreamsHeadersOverWebsocket(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	block, _, _, _ := multiTxBlock(t, 7, blockHash, time.Unix(1_700_000_000, 0))
	executed := utils.NewAtomicSend(atypes.ExecutedBlock{Number: 6})
	subscribed := make(chan struct{}, 1)
	backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		require.Equal(t, coretypes.Int64(7), *req.Height)
		return block, nil
	})
	backend.executedBlocks = func() (utils.AtomicRecv[atypes.ExecutedBlock], error) {
		subscribed <- struct{}{}
		return executed.Subscribe(), nil
	}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(websocketHandler(handler))
	t.Cleanup(server.Close)
	client, err := ethclient.DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
	require.NoError(t, err)
	t.Cleanup(client.Close)

	headers := make(chan *ethtypes.Header, 1)
	sub, err := client.SubscribeNewHead(t.Context(), headers)
	require.NoError(t, err)
	<-subscribed

	executed.Store(atypes.ExecutedBlock{Number: 7, GasUsed: 43_500})

	select {
	case header := <-headers:
		require.Equal(t, big.NewInt(7), header.Number)
		require.Equal(t, uint64(43_500), header.GasUsed)
		require.Equal(t, uint64(35_000_000), header.GasLimit)
		require.Equal(t, uint64(1_700_000_000), header.Time)
	case err := <-sub.Err():
		t.Fatalf("subscription failed: %v", err)
	}

	sub.Unsubscribe()
}

// TestNewHeadsReadsReceiptsForBlockTheWatchPassed covers a subscriber that
// observes the watch only after it moved two blocks: the passed block's gasUsed
// is read from the receipt store, the current one's from the commit.
func TestNewHeadsReadsReceiptsForBlockTheWatchPassed(t *testing.T) {
	block7, _, _, filledStore := multiTxBlock(t, 7, common.HexToHash("0xabcd"), time.Unix(1_700_000_000, 0))
	block8, _, _, _ := multiTxBlock(t, 8, common.HexToHash("0xabce"), time.Unix(1_700_000_001, 0))
	store := evmonly.NewMemoryReceiptStore()
	copyReceipts(t, filledStore, store, block7)
	executed := utils.NewAtomicSend(atypes.ExecutedBlock{Number: 6})
	subscribed := make(chan struct{}, 1)
	backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		switch *req.Height {
		case 7:
			return block7, nil
		case 8:
			return block8, nil
		}
		return nil, fmt.Errorf("unexpected height %d", *req.Height)
	})
	backend.executedBlocks = func() (utils.AtomicRecv[atypes.ExecutedBlock], error) {
		subscribed <- struct{}{}
		return executed.Subscribe(), nil
	}
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(websocketHandler(handler))
	t.Cleanup(server.Close)
	client, err := ethclient.DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
	require.NoError(t, err)
	t.Cleanup(client.Close)

	headers := make(chan *ethtypes.Header, 2)
	sub, err := client.SubscribeNewHead(t.Context(), headers)
	require.NoError(t, err)
	<-subscribed

	executed.Store(atypes.ExecutedBlock{Number: 8, GasUsed: 21_000})

	want := []struct {
		number  int64
		gasUsed uint64
	}{{7, 43_500}, {8, 21_000}}
	for _, w := range want {
		select {
		case header := <-headers:
			require.Equal(t, big.NewInt(w.number), header.Number)
			require.Equal(t, w.gasUsed, header.GasUsed)
		case err := <-sub.Err():
			t.Fatalf("subscription failed: %v", err)
		}
	}

	sub.Unsubscribe()
}

func TestNewHeadsRejectedOverPlainHTTP(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, nil)
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	_, err = client.EthSubscribe(t.Context(), make(chan map[string]any), "newHeads")
	require.ErrorIs(t, err, ethrpc.ErrNotificationsUnsupported)
}

// copyReceipts writes block's receipts from src into dst.
func copyReceipts(t *testing.T, src, dst receipt.ReceiptStore, block *coretypes.ResultBlock) {
	t.Helper()
	ctx := sdk.Context{}.WithContext(t.Context()).WithBlockHeight(block.Block.Height)
	records := make([]receipt.ReceiptRecord, 0, len(block.Block.Txs))
	for i, raw := range block.Block.Txs {
		tx, err := decodeBlockTx(raw, block.Block.Height, i)
		require.NoError(t, err)
		stored, err := src.GetReceipt(ctx, tx.Hash())
		require.NoError(t, err)
		records = append(records, receipt.ReceiptRecord{TxHash: tx.Hash(), Receipt: stored})
	}
	require.NoError(t, dst.SetReceipts(ctx, records))
}
