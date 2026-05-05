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
	coord := &Coordinator{
		requests: requests,
		done:     done,
	}
	go coord.run()

	require.Zero(t, cap(coord.requests))

	firstResp := make(chan writeResp)
	coord.requests <- writeReq{
		inputs: []parquet.ReceiptInput{testReceiptInput(1, common.HexToHash("0x1"))},
		resp:   firstResp,
	}
	time.Sleep(10 * time.Millisecond)

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- coord.Flush()
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second request completed before first unblocked: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	require.Error(t, (<-firstResp).err)
	require.NoError(t, <-secondDone)
	require.NoError(t, coord.Close())
}
