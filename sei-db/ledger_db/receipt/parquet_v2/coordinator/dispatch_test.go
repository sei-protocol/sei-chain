package coordinator

import (
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
	require.Equal(t, uint64(3), reader.maxBlocksPerFile)
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
