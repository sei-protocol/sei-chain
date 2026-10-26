package evmonlyapp

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// newInitializedInMemoryEVMOnlyTestApp returns an initialized application over storage that keeps
// state and receipts in memory.
func newInitializedInMemoryEVMOnlyTestApp(t *testing.T) *evmOnlyApplication {
	t.Helper()
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage, err := bootstrap.NewGigaStorageManagerWithMemoryState(
		storageConfig,
		evmonly.NewFlatKVMemoryStore(),
		evmonly.NewQueuedMemoryReceiptStore(),
	)
	require.NoError(t, err)
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		nil,
		storage,
		evmonly.NewFlatKVChangeSetEncoder(),
		gigaconfig.DefaultConfig.Execution,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		settler, ok := app.(*evmOnlyApplication)
		require.True(t, ok)
		require.NoError(t, settler.AwaitCommits())
		require.NoError(t, storage.Close())
	})
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	return app.(*evmOnlyApplication)
}

func TestEVMOnlyApplicationWithInMemoryStateMatchesDiskStorage(t *testing.T) {
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	tx := decodeEVMOnlyTestTx(t, raw)
	request := &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	}
	disk := newInitializedEVMOnlyTestApp(t)
	memory := newInitializedInMemoryEVMOnlyTestApp(t)
	require.True(t, memory.storage.InMemoryState())

	diskResponse, err := disk.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	memoryResponse, err := memory.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, diskResponse.AppHash, memoryResponse.AppHash)
	_, err = disk.Commit(t.Context())
	require.NoError(t, err)
	_, err = memory.Commit(t.Context())
	require.NoError(t, err)

	require.Equal(t, int64(1), memory.LastBlockHeight())
	require.Equal(t, disk.EvmNonce(sender), memory.EvmNonce(sender))
	require.Equal(t, disk.EvmBalance(sender, nil), memory.EvmBalance(sender, nil))
	receiptDB := memory.storage.ReceiptDB()
	waitForReceiptVersion(t, receiptDB, 1)
	receiptCtx := sdk.NewContext(nil, tmproto.Header{Height: 1}, false).WithContext(t.Context())
	gotReceipt, err := receiptDB.GetReceipt(receiptCtx, tx.Hash())
	require.NoError(t, err)
	require.Equal(t, tx.Hash().Hex(), gotReceipt.TxHashHex)
	require.Equal(t, uint64(1), gotReceipt.BlockNumber)
}

func TestEVMOnlyApplicationWithInMemoryStateResumesFromItsCursor(t *testing.T) {
	memory := newInitializedInMemoryEVMOnlyTestApp(t)
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	response, err := memory.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	_, err = memory.Commit(t.Context())
	require.NoError(t, err)
	require.NoError(t, memory.AwaitCommits())

	cursor, err := loadEVMOnlyCursor(memory.storage.Versions())
	require.NoError(t, err)
	got, ok := cursor.Get()
	require.True(t, ok)
	require.Equal(t, int64(1), got.height)
	require.Equal(t, common.BytesToHash(response.AppHash), got.appHash)
}
