package coordinator

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestSetMaxBlocksPerFileUpdatesReaderState(t *testing.T) {
	reader, err := NewReaderWithMaxBlocksPerFile(t.TempDir(), 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	resp := make(chan error, 1)
	coord := &Coordinator{
		config: parquet.StoreConfig{
			MaxBlocksPerFile: 10,
		},
		reader: reader,
	}

	coord.handleSetMaxBlocksPerFile(setMaxBlocksPerFileReq{
		maxBlocksPerFile: 3,
		resp:             resp,
	})

	require.NoError(t, <-resp)
	require.Equal(t, uint64(3), coord.config.MaxBlocksPerFile)
	require.Equal(t, uint64(3), coord.cacheRotateInterval.Load())
	require.Equal(t, uint64(3), reader.maxBlocksPerFile)
}

func TestSetMaxBlocksPerFileRejectsZero(t *testing.T) {
	reader, err := NewReaderWithMaxBlocksPerFile(t.TempDir(), 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	resp := make(chan error, 1)
	coord := &Coordinator{
		config: parquet.StoreConfig{
			MaxBlocksPerFile: 10,
		},
		reader: reader,
	}
	coord.cacheRotateInterval.Store(10)

	coord.handleSetMaxBlocksPerFile(setMaxBlocksPerFileReq{
		maxBlocksPerFile: 0,
		resp:             resp,
	})

	require.ErrorContains(t, <-resp, "max blocks per file must be greater than 0")
	require.Equal(t, uint64(10), coord.config.MaxBlocksPerFile)
	require.Equal(t, uint64(10), coord.cacheRotateInterval.Load())
	require.Equal(t, uint64(10), reader.maxBlocksPerFile)
}

func TestHandleCloseReleasesAllResourcesOnFlushError(t *testing.T) {
	coord, err := New(parquet.StoreConfig{
		DBDirectory:      t.TempDir(),
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)

	require.NotNil(t, coord.wal)
	require.NotNil(t, coord.reader)

	require.NoError(t, coord.WriteReceipts(1, []parquet.ReceiptInput{
		testReceiptInput(1, common.HexToHash("0x1")),
	}))
	require.NotNil(t, coord.receiptWriter)
	require.NotNil(t, coord.receiptFile)

	coord.SetFaultHooks(&parquet.FaultHooks{
		BeforeFlush: func(uint64) error { return errors.New("injected flush failure") },
	})

	closeErr := coord.Close()
	require.Error(t, closeErr)
	require.ErrorContains(t, closeErr, "injected flush failure")

	require.Nil(t, coord.wal, "WAL must be released even when flushOpenFile errors")
	require.Nil(t, coord.reader, "reader must be released even when flushOpenFile errors")
	require.Nil(t, coord.receiptWriter)
	require.Nil(t, coord.logWriter)
	require.Nil(t, coord.receiptFile)
	require.Nil(t, coord.logFile)
}

func TestCloseAfterReceiptFlushFailureReplaysWALOnRestart(t *testing.T) {
	dir := t.TempDir()
	coord, err := New(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)

	txHash := common.HexToHash("0x1")
	require.NoError(t, coord.WriteReceipts(1, []parquet.ReceiptInput{
		testReceiptInput(1, txHash),
	}))

	injectedErr := errors.New("injected post-receipt flush failure")
	coord.SetFaultHooks(&parquet.FaultHooks{
		AfterReceiptFlush: func(uint64) error { return injectedErr },
	})

	require.ErrorIs(t, coord.Close(), injectedErr)

	reopened, err := New(parquet.StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 4,
		WALConverter:     replayConverterForTest,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })

	require.Equal(t, []ReplayedBlock{{
		BlockNumber: 1,
		TxHashes:    []common.Hash{txHash},
	}}, reopened.ReplayedBlocks(), "restart must replay WAL entries whose logs may not have flushed")
}

func TestCloseReturnsSameErrorToRepeatCallers(t *testing.T) {
	coord, err := New(parquet.StoreConfig{
		DBDirectory:      t.TempDir(),
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)

	require.NoError(t, coord.WriteReceipts(1, []parquet.ReceiptInput{
		testReceiptInput(1, common.HexToHash("0x1")),
	}))

	coord.SetFaultHooks(&parquet.FaultHooks{
		BeforeFlush: func(uint64) error { return errors.New("injected flush failure") },
	})

	first := coord.Close()
	second := coord.Close()
	require.Error(t, first)
	require.Error(t, second, "second Close() must surface the original close error, not nil")
	require.Equal(t, first, second)
}

func TestUnbufferedRequestsApplyBackpressure(t *testing.T) {
	requests := make(chan coordRequest)
	done := make(chan struct{})
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	writeErr := errors.New("released write")
	coord := &Coordinator{
		requests: requests,
		done:     done,
		wal: &blockingWAL{
			started: writeStarted,
			release: releaseWrite,
			err:     writeErr,
		},
	}
	go coord.run()

	require.Zero(t, cap(coord.requests))

	firstResp := make(chan writeResp, 1)
	coord.requests <- writeReq{
		inputs: []parquet.ReceiptInput{testReceiptInput(1, common.HexToHash("0x1"))},
		resp:   firstResp,
	}
	<-writeStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- coord.Flush()
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second request completed before first unblocked: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseWrite)
	require.ErrorIs(t, (<-firstResp).err, writeErr)
	require.NoError(t, <-secondDone)
	require.NoError(t, coord.Close())
}

type blockingWAL struct {
	recordingWAL
	started chan<- struct{}
	release <-chan struct{}
	err     error
}

func (w *blockingWAL) Write(parquet.WALEntry) error {
	close(w.started)
	<-w.release
	return w.err
}
