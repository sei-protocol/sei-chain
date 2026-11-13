package evmonlyapp

import (
	"context"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// blockRecorder is a result sink that records the blocks it is handed.
type blockRecorder struct {
	blocks []evmonly.PreparedBlock
}

func (r *blockRecorder) StoreBlockResult(_ context.Context, block evmonly.PreparedBlock, _ *evmonly.BlockResult, release func()) error {
	r.blocks = append(r.blocks, block)
	release()
	return nil
}

func TestEVMOnlyApplicationAppliesExecutorOptions(t *testing.T) {
	storageConfig, err := seidbconfig.AutobahnStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage := openEVMOnlyTestStorage(t, storageConfig)
	recorder := &blockRecorder{}
	app, err := NewEVMOnlyApplication(
		evmOnlyTestChainID,
		nil,
		storage,
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.None[string](),
		evmonly.WithResultSink(recorder),
	)
	require.NoError(t, err)
	t.Cleanup(func() { closeEVMOnlyTestApp(t, app, storage) })
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)

	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1, raw))
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))

	require.Len(t, recorder.blocks, 2)
	require.Equal(t, uint64(1), recorder.blocks[0].Context.Number)
	require.Len(t, recorder.blocks[0].Txs, 1)
	require.Equal(t, sender, recorder.blocks[0].Txs[0].Sender)
	require.Equal(t, uint64(2), recorder.blocks[1].Context.Number)
	require.Empty(t, recorder.blocks[1].Txs)
}
