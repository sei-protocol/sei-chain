package parquet_v2

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestRotationBoundaryPrimitives(t *testing.T) {
	coord := &coordinator{
		config: parquet.StoreConfig{MaxBlocksPerFile: 500},
	}

	resp := make(chan bool, 1)
	coord.handleIsRotationBoundary(isRotationBoundaryReq{blockNumber: 0, resp: resp})
	require.True(t, <-resp)

	resp = make(chan bool, 1)
	coord.handleIsRotationBoundary(isRotationBoundaryReq{blockNumber: 500, resp: resp})
	require.True(t, <-resp)

	resp = make(chan bool, 1)
	coord.handleIsRotationBoundary(isRotationBoundaryReq{blockNumber: 501, resp: resp})
	require.False(t, <-resp)

	coord.config.MaxBlocksPerFile = 0
	resp = make(chan bool, 1)
	coord.handleIsRotationBoundary(isRotationBoundaryReq{blockNumber: 500, resp: resp})
	require.False(t, <-resp)
}

func TestAlignedFileStartBlock(t *testing.T) {
	require.Equal(t, uint64(5000), alignedFileStartBlock(5234, 500))
	require.Equal(t, uint64(5000), alignedFileStartBlock(5000, 500))
	require.Equal(t, uint64(0), alignedFileStartBlock(499, 500))
	require.Equal(t, uint64(5234), alignedFileStartBlock(5234, 0))
}
