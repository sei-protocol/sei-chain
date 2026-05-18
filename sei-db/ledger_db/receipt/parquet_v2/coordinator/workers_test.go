package coordinator

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

// newReadCoordinator builds a coordinator with closed parquet files on
// disk and a real reader, ready to dispatch reads through the worker
// pool. Returns the coordinator and the directory.
func newReadCoordinator(t *testing.T, starts ...uint64) (*Coordinator, string) {
	t.Helper()
	dir := t.TempDir()
	closedFiles := writeClosedFileSet(t, dir, starts...)
	reader, err := NewReaderWithMaxBlocksPerFile(dir, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	coord := &Coordinator{
		config: parquet.StoreConfig{
			KeepRecent:       4,
			MaxBlocksPerFile: 4,
		},
		basePath:       dir,
		closedFiles:    closedFiles,
		latestVersion:  int64(starts[len(starts)-1]) + 4,
		reader:         reader,
		tempWriteCache: make(map[common.Hash][]tempReceipt),
	}
	bootstrapWorkersForTest(coord)
	t.Cleanup(func() { coord.shutdownWorkers() })
	return coord, dir
}

// dispatchReadInBlock issues a read through the handler and returns the
// response channel without blocking on it.
func dispatchReadInBlock(coord *Coordinator, txHash common.Hash, blockNumber uint64) chan readReceiptResp {
	resp := make(chan readReceiptResp, 1)
	coord.handleReadByTxHashInBlock(readByTxHashInBlockReq{
		ctx:         context.Background(),
		txHash:      txHash,
		blockNumber: blockNumber,
		resp:        resp,
	})
	return resp
}

func TestReadDispatchIncrementsAndDecrementsRefcount(t *testing.T) {
	coord, dir := newReadCoordinator(t, 0, 4)

	// Pre-acquire a refcount on file_0 by holding a reader gate via
	// dispatching a read against block 1 (file_0). The handler increments
	// the refcount synchronously before dispatching the lambda.
	resp := dispatchReadInBlock(coord, common.BigToHash(big.NewInt(1)), 1)
	target := filepath.Join(dir, "receipts_0.parquet")

	// Refcount becomes visible immediately because acquireReadRefs runs
	// on the same goroutine as the handler call (the test goroutine
	// here, not the coordinator's run loop — we're driving the handler
	// directly).
	require.Equal(t, 1, inFlightReadsForTest(coord, target))

	// Wait for the read to complete and the lambda to send readDoneMsg,
	// then drain it through handleControl.
	<-resp
	quiesceWorkersForTest(coord)

	require.Equal(t, 0, inFlightReadsForTest(coord, target))
}

func TestPruneWhileReadingDefersDeletion(t *testing.T) {
	coord, dir := newReadCoordinator(t, 0, 4)
	receiptPath := filepath.Join(dir, "receipts_0.parquet")
	logPath := filepath.Join(dir, "logs_0.parquet")

	// Manually pre-bump the refcount to simulate an in-flight read on
	// file_0 that the test will later release. This avoids racing on
	// when the read lambda actually completes.
	coord.acquireReadRefs([]string{receiptPath, logPath})

	// Force a prune tick. file_0 is eligible (latestVersion=8,
	// keepRecent=4 → pruneBefore=4 → file_0 ages out). With a refcount
	// >0 the file must move to pendingPrune, not be deleted yet.
	coord.handlePruneTick()
	quiesceWorkersForTest(coord)

	require.Equal(t, 1, pendingPruneCountForTest(coord))
	require.FileExists(t, receiptPath)
	require.FileExists(t, logPath)

	// closedFiles must already exclude the pruned file so subsequent
	// snapshots don't pick it up.
	for _, f := range coord.closedFiles {
		require.NotEqual(t, uint64(0), f.startBlock, "closedFiles must not include the pending-prune file")
	}

	// Releasing the refs triggers the deferred prune via flushPendingPrune.
	coord.releaseReadRefs([]string{receiptPath, logPath})
	quiesceWorkersForTest(coord)

	require.Equal(t, 0, pendingPruneCountForTest(coord))
	require.NoFileExists(t, receiptPath)
	require.NoFileExists(t, logPath)
}

func TestNewReadAfterPruneTickExcludesPendingFile(t *testing.T) {
	coord, dir := newReadCoordinator(t, 0, 4)
	receiptPath := filepath.Join(dir, "receipts_0.parquet")
	logPath := filepath.Join(dir, "logs_0.parquet")

	coord.acquireReadRefs([]string{receiptPath, logPath})
	coord.handlePruneTick()
	quiesceWorkersForTest(coord)
	require.Equal(t, 1, pendingPruneCountForTest(coord))

	// A new GetLogs against the full window must not see the pending-
	// prune file in its snapshot.
	from := uint64(0)
	to := uint64(100)
	snapshot := coord.filteredLogFilesSnapshot(parquet.LogFilter{FromBlock: &from, ToBlock: &to})
	for _, p := range snapshot {
		require.NotEqual(t, logPath, p, "new read snapshot must not include pending-prune file")
	}

	coord.releaseReadRefs([]string{receiptPath, logPath})
	quiesceWorkersForTest(coord)
}

func TestReadPoolDispatchesReadsConcurrently(t *testing.T) {
	coord, _ := newReadCoordinator(t, 0, 4)

	// In production handlers run serially on the coordinator goroutine.
	// We dispatch from a single goroutine here for the same reason —
	// what we're proving is that the WORKER POOL services them in
	// parallel, not that the handler is reentrant. With a deep readChan
	// the handler returns immediately, so dispatching N reads back-to-
	// back enqueues all N before any worker has a chance to drain the
	// channel.
	const n = 8
	resps := make([]chan readReceiptResp, n)
	for i := 0; i < n; i++ {
		resps[i] = dispatchReadInBlock(coord, common.BigToHash(big.NewInt(int64(1+(i%4)))), uint64(1+(i%4)))
	}
	for i := 0; i < n; i++ {
		select {
		case <-resps[i]:
		case <-time.After(5 * time.Second):
			t.Fatalf("read %d did not complete", i)
		}
	}
	quiesceWorkersForTest(coord)
}

func TestShutdownDrainsActiveReads(t *testing.T) {
	coord, _ := newReadCoordinator(t, 0, 4)

	const n = 4
	resps := make([]chan readReceiptResp, n)
	for i := 0; i < n; i++ {
		resps[i] = dispatchReadInBlock(coord, common.BigToHash(big.NewInt(int64(1+(i%4)))), uint64(1+(i%4)))
	}

	// Trigger shutdown immediately. shutdownWorkers must drain in-flight
	// lambdas — every response should still arrive.
	done := make(chan struct{})
	go func() {
		coord.shutdownWorkers()
		close(done)
	}()
	for i := 0; i < n; i++ {
		select {
		case <-resps[i]:
		case <-time.After(5 * time.Second):
			t.Fatalf("read %d not drained on shutdown", i)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdownWorkers did not return")
	}
}
