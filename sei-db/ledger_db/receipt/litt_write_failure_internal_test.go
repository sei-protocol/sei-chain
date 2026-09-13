package receipt

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/stretchr/testify/require"
)

var errIndexCommit = errors.New("injected index commit failure")

// failingIndex is the store's log index with its batch commits made to fail on demand. Committing
// the index is the one step of a receipt write with no other way to fail in a test, and holding the
// commit is what lets a test queue a block behind the one that is failing.
type failingIndex struct {
	dbtypes.KeyValueDB
	failing     atomic.Bool
	entered     chan struct{} // closed once a failing commit has been reached
	enteredOnce sync.Once     // more than one commit may fail, and entered closes for the first
	release     chan struct{} // closed to let that commit return its error
}

func (f *failingIndex) NewBatch() dbtypes.Batch {
	return &failingBatch{Batch: f.KeyValueDB.NewBatch(), index: f}
}

type failingBatch struct {
	dbtypes.Batch
	index *failingIndex
}

func (b *failingBatch) Commit(opts dbtypes.WriteOptions) error {
	if b.index.failing.Load() {
		b.index.enteredOnce.Do(func() { close(b.index.entered) })
		<-b.index.release
		return errIndexCommit
	}
	return b.Batch.Commit(opts)
}

// TestWriteFailureHoldsTheHeadAgainstAQueuedBlock covers what a failed background write owes the
// blocks already queued behind it. Applying one would commit its own version marker and publish a
// head above the block that never landed: reads of the missing block report no logs and no such
// transaction, and recovery converges on the published head, so the gap never refills.
//
// The block behind the failure is queued while the failing commit is held, which is the ordering
// that makes this reachable — SetReceipts refuses new blocks once the failure is visible.
func TestWriteFailureHoldsTheHeadAgainstAQueuedBlock(t *testing.T) {
	s, closeStore := setupLittCtxStore(t)
	defer closeStore()

	addr := common.HexToAddress("0xfa11")
	topic := common.HexToHash("0xfa12")

	index := &failingIndex{
		KeyValueDB: s.index,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	s.index = index

	// Block 1 lands, so there is a real head for the failure to hold.
	writeOneReceipt(t, s, 1, addr, topic)
	requireReceiptVersion(t, s, 1)

	// Block 2 reaches its commit and stops there, still holding the writer.
	index.failing.Store(true)
	writeOneReceipt(t, s, 2, addr, topic)
	<-index.entered

	// Block 3 would commit cleanly and carry a marker naming it the head. Queued now, while block 2
	// is mid-commit, it is past the refusal in SetReceipts and only the writer can hold it back.
	index.failing.Store(false)
	writeOneReceipt(t, s, 3, addr, topic)

	close(index.release)

	// Close drains, so the writer has decided about block 3 by the time this returns.
	require.ErrorIs(t, s.Close(), errIndexCommit)
	require.Equal(t, int64(1), s.LatestVersion(),
		"the head must not move past a block whose receipts were never written")
}

// TestWriteFailureLatches covers the failure reaching every later caller rather than the first one
// to ask, which is what lets both a commit and Close act on it.
func TestWriteFailureLatches(t *testing.T) {
	s, closeStore := setupLittCtxStore(t)
	defer closeStore()

	addr := common.HexToAddress("0xfa21")
	topic := common.HexToHash("0xfa22")

	index := &failingIndex{
		KeyValueDB: s.index,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	s.index = index
	close(index.release)

	index.failing.Store(true)
	writeOneReceipt(t, s, 1, addr, topic)
	require.Eventually(t, func() bool { return s.writeFailure() != nil }, 5*time.Second, time.Millisecond)

	txHash, rcpt := littCtxTestReceipt(2, 0, addr, topic, 1)
	require.ErrorIs(t, s.SetReceipts(newTestCtxAtHeight(2), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}),
		errIndexCommit, "a commit after a failed write must be refused rather than queued")
	require.ErrorIs(t, s.writeFailure(), errIndexCommit, "reading the failure must not consume it")
	require.ErrorIs(t, s.Close(), errIndexCommit, "Close must report it too")
}

// TestWriteAfterCloseIsRefused covers a commit that races shutdown. The writer has drained and gone
// by then, so a write it accepted would sit in a channel nobody reads, and one arriving on a full
// queue would never return — inside a commit, which hangs the node rather than failing it.
func TestWriteAfterCloseIsRefused(t *testing.T) {
	s, _ := setupLittCtxStore(t)
	require.NoError(t, s.Close())

	txHash, rcpt := littCtxTestReceipt(1, 0, common.HexToAddress("0xfa31"), common.HexToHash("0xfa32"), 1)
	err := s.SetReceipts(newTestCtxAtHeight(1), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}})
	require.ErrorIs(t, err, ErrStoreClosed)
}

// TestWriteAfterCloseIsRefusedWithAFullQueue is the same refusal with no room left to send into,
// which is the case that would otherwise block forever rather than return.
func TestWriteAfterCloseIsRefusedWithAFullQueue(t *testing.T) {
	s, _ := setupLittCtxStore(t)
	require.NoError(t, s.Close())

	// Leftovers with no writer behind them: the send has nowhere to go and nobody to take it.
	for len(s.writes) < cap(s.writes) {
		s.writes <- receiptWrite{height: 1}
	}

	txHash, rcpt := littCtxTestReceipt(1, 0, common.HexToAddress("0xfa41"), common.HexToHash("0xfa42"), 1)
	done := make(chan error, 1)
	go func() {
		done <- s.SetReceipts(newTestCtxAtHeight(1), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}})
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrStoreClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("a write into a full queue on a closed store never returned")
	}
}

// TestWriteRacingCloseIsEitherAppliedOrRefused covers the ordering the two tests above cannot reach,
// where a write is admitted while Close is running rather than after it has returned. Admission and
// shutdown have to be mutually exclusive: a write that returns success must have reached a writer
// that was still running, so the height it reports is one the store actually holds.
func TestWriteRacingCloseIsEitherAppliedOrRefused(t *testing.T) {
	for attempt := range 50 {
		s, _ := setupLittCtxStore(t)

		addr := common.HexToAddress("0xfa51")
		topic := common.HexToHash("0xfa52")
		txHash, rcpt := littCtxTestReceipt(1, 0, addr, topic, 1)

		started := make(chan struct{})
		result := make(chan error, 1)
		go func() {
			close(started)
			result <- s.SetReceipts(newTestCtxAtHeight(1), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}})
		}()
		<-started
		require.NoError(t, s.Close())

		if err := <-result; err != nil {
			require.ErrorIs(t, err, ErrStoreClosed, "attempt %d", attempt)
			continue
		}
		// Accepted, so the writer must have applied it before Close let the writer go.
		require.Equal(t, int64(1), s.LatestVersion(),
			"attempt %d: a write that reported success must have been applied", attempt)
	}
}

func writeOneReceipt(t *testing.T, s *littReceiptStore, block uint64, addr common.Address, topic common.Hash) {
	t.Helper()
	txHash, rcpt := littCtxTestReceipt(block, 0, addr, topic, 1)
	require.NoError(t, s.SetReceipts(newTestCtxAtHeight(block), []ReceiptRecord{{TxHash: txHash, Receipt: rcpt}}))
}
