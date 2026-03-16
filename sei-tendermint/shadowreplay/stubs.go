package shadowreplay

import (
	"context"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Minimal no-op stubs so shadow replay can construct a BlockExecutor and
// call ApplyBlock on the unified production code path.

// nopMempool satisfies mempool.Mempool with no-ops.
type nopMempool struct{}

var _ mempool.Mempool = nopMempool{}

func (nopMempool) CheckTx(context.Context, types.Tx, func(*abci.ResponseCheckTx), mempool.TxInfo) error {
	return nil
}
func (nopMempool) RemoveTxByKey(types.TxKey) error                                  { return nil }
func (nopMempool) HasTx(types.TxKey) bool                                           { return false }
func (nopMempool) GetTxsForKeys([]types.TxKey) types.Txs                            { return nil }
func (nopMempool) SafeGetTxsForKeys([]types.TxKey) (types.Txs, []types.TxKey)       { return nil, nil }
func (nopMempool) ReapMaxBytesMaxGas(_, _, _ int64) types.Txs                        { return nil }
func (nopMempool) ReapMaxTxs(int) types.Txs                                          { return nil }
func (nopMempool) Lock()                                                              {}
func (nopMempool) Unlock()                                                            {}
func (nopMempool) Update(context.Context, int64, types.Txs, []*abci.ExecTxResult, mempool.PreCheckFunc, mempool.PostCheckFunc, bool) error {
	return nil
}
func (nopMempool) Flush()                        {}
func (nopMempool) TxsAvailable() <-chan struct{}  { return make(chan struct{}) }
func (nopMempool) EnableTxsAvailable()            {}
func (nopMempool) Size() int                      { return 0 }
func (nopMempool) SizeBytes() int64               { return 0 }
func (nopMempool) TxStore() *mempool.TxStore      { return nil }

// nopBlockStore satisfies sm.BlockStore with no-ops.
type nopBlockStore struct{}

var _ sm.BlockStore = nopBlockStore{}

func (nopBlockStore) Base() int64                                           { return 0 }
func (nopBlockStore) Height() int64                                         { return 0 }
func (nopBlockStore) Size() int64                                           { return 0 }
func (nopBlockStore) LoadBaseMeta() *types.BlockMeta                        { return nil }
func (nopBlockStore) LoadBlockMeta(int64) *types.BlockMeta                  { return nil }
func (nopBlockStore) LoadBlock(int64) *types.Block                          { return nil }
func (nopBlockStore) SaveBlock(*types.Block, *types.PartSet, *types.Commit) {}
func (nopBlockStore) PruneBlocks(int64) (uint64, error)                     { return 0, nil }
func (nopBlockStore) LoadBlockByHash([]byte) *types.Block                   { return nil }
func (nopBlockStore) LoadBlockMetaByHash([]byte) *types.BlockMeta           { return nil }
func (nopBlockStore) LoadBlockPart(int64, int) *types.Part                  { return nil }
func (nopBlockStore) LoadBlockCommit(int64) *types.Commit                   { return nil }
func (nopBlockStore) LoadSeenCommit() *types.Commit                         { return nil }
func (nopBlockStore) DeleteLatestBlock() error                              { return nil }
