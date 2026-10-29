package gigareceipt

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

// heldIndex is the store's log index with each batch commit held until release closes.
type heldIndex struct {
	dbtypes.KeyValueDB
	entered chan struct{} // receives once per commit that reaches the hold
	release chan struct{}
}

func (h *heldIndex) NewBatch() dbtypes.Batch {
	return &heldBatch{Batch: h.KeyValueDB.NewBatch(), index: h}
}

type heldBatch struct {
	dbtypes.Batch
	index *heldIndex
}

func (b *heldBatch) Commit(opts dbtypes.WriteOptions) error {
	b.index.entered <- struct{}{}
	<-b.index.release
	return b.Batch.Commit(opts)
}

func openPipelinedStore(t *testing.T, dir string) *littReceiptStore {
	t.Helper()
	cfg := littIdxCfg(dir)
	cfg.PipelinedIndexCommits = true
	_, storeKey := newTestContext()
	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store.(*littReceiptStore)
}

// TestPipelinedWriteAcceptsAnotherPartOfTheBlockInFlight covers a second part of a block written while
// the first part's index commit is still in flight: it waits for that commit rather than being refused.
func TestPipelinedWriteAcceptsAnotherPartOfTheBlockInFlight(t *testing.T) {
	s := openPipelinedStore(t, t.TempDir())
	addr := common.HexToAddress("0xfb11")
	topic := common.HexToHash("0xfb12")
	writeOneReceipt(t, s, 1, addr, topic)
	requireReceiptVersion(t, s, 1)

	index := &heldIndex{KeyValueDB: s.index, entered: make(chan struct{}, 2), release: make(chan struct{})}
	s.index = index

	txHash, rcpt := littCtxTestReceipt(2, 0, addr, topic, 1)
	require.NoError(t, s.writeReceipts(2, []receipt.ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))
	<-index.entered

	result := make(chan error, 1)
	go func() {
		txHash, rcpt := littCtxTestReceipt(2, 1, addr, topic, 1)
		result <- s.writeReceipts(2, []receipt.ReceiptRecord{{TxHash: txHash, Receipt: rcpt}})
	}()
	select {
	case err := <-result:
		close(index.release)
		t.Fatalf("second part returned before the first part's commit landed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(index.release)
	require.NoError(t, <-result)
	require.NoError(t, s.Close())
	require.Equal(t, int64(2), s.LatestVersion())
}

// TestPipelinedCommitFailureHoldsTheHead covers a background index commit that fails: the next block is
// refused, Close reports the failure, and neither the in-memory nor the persisted head passes the
// failed block.
func TestPipelinedCommitFailureHoldsTheHead(t *testing.T) {
	dir := t.TempDir()
	s := openPipelinedStore(t, dir)
	addr := common.HexToAddress("0xfb21")
	topic := common.HexToHash("0xfb22")
	writeOneReceipt(t, s, 1, addr, topic)
	requireReceiptVersion(t, s, 1)

	index := &failingIndex{
		KeyValueDB: s.index,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	s.index = index
	close(index.release)

	index.failing.Store(true)
	writeOneReceipt(t, s, 2, addr, topic)
	require.Eventually(t, func() bool { return s.writeFailure() != nil }, 5*time.Second, time.Millisecond)

	index.failing.Store(false)
	txHash, rcpt := littCtxTestReceipt(3, 0, addr, topic, 1)
	require.ErrorIs(t, s.SetReceipts(newTestCtxAtHeight(3),
		[]receipt.ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}), errIndexCommit)
	require.ErrorIs(t, s.Close(), errIndexCommit)
	require.Equal(t, int64(1), s.LatestVersion())

	require.Equal(t, int64(1), openPipelinedStore(t, dir).LatestVersion())
}
