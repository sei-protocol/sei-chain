package evmonlyapp

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestBlockTxsHashes(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	require.Zero(t, app.EvmBlockNumber())
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	first := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)
	second := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1)
	third := signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 2)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, first))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2, first, second, second, third))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
	store := app.(*evmOnlyApplication).storage.ReceiptDB()
	require.NoError(t, store.Flush())
	proxy := abevm.NewProxy(app)
	require.Equal(t, int64(3), proxy.EvmBlockNumber())
	for _, height := range []int64{-1, 0} {
		_, err := proxy.BlockTxsHashes(height)
		require.Error(t, err)
	}

	hashes, err := proxy.BlockTxsHashes(1)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{crypto.Keccak256Hash(first)}, hashes)
	hashes, err = proxy.BlockTxsHashes(2)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{crypto.Keccak256Hash(second), crypto.Keccak256Hash(third)}, hashes)
	hashes, err = proxy.BlockTxsHashes(3)
	require.NoError(t, err)
	require.Empty(t, hashes)
	_, err = proxy.BlockTxsHashes(4)
	require.ErrorIs(t, err, coretypes.ErrHeightExceedsChainHead)
	require.NoError(t, store.PruneHistory(3))
	_, err = proxy.BlockTxsHashes(2)
	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)
}

func TestBlockTxsHashesWithoutReceipts(t *testing.T) {
	config, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	config.ReceiptDBConfig.Enable = false
	app := newEVMOnlyTestAppWithStorage(t, nil, gigaconfig.DefaultConfig.Execution, config)
	_, err = app.BlockTxsHashes(1)
	require.ErrorIs(t, err, receiptdb.ErrNotConfigured)
	require.NoError(t, app.InitChain(evmOnlyTestInitChain()))
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1))
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Zero(t, app.EvmBlockNumber())
}
