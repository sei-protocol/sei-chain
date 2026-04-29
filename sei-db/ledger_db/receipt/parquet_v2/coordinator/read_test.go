package coordinator

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestReadByTxHashHitsTempCache(t *testing.T) {
	txHash := common.HexToHash("0xabc")
	coord := &Coordinator{
		tempWriteCache: map[common.Hash][]tempReceipt{
			txHash: {
				{blockNumber: 10, writeOrdinal: 0, receiptBytes: []byte("first")},
				{blockNumber: 10, writeOrdinal: 1, receiptBytes: []byte("second")},
				{blockNumber: 11, writeOrdinal: 2, receiptBytes: []byte("third")},
			},
		},
	}

	resp := make(chan readReceiptResp, 1)
	coord.handleReadByTxHash(readByTxHashReq{
		ctx:    context.Background(),
		txHash: txHash,
		resp:   resp,
	})
	result := <-resp
	require.NoError(t, result.err)
	require.Equal(t, uint64(10), result.result.BlockNumber)
	require.Equal(t, []byte("first"), result.result.ReceiptBytes)

	resp = make(chan readReceiptResp, 1)
	coord.handleReadByTxHashInBlock(readByTxHashInBlockReq{
		ctx:         context.Background(),
		txHash:      txHash,
		blockNumber: 11,
		resp:        resp,
	})
	result = <-resp
	require.NoError(t, result.err)
	require.Equal(t, uint64(11), result.result.BlockNumber)
	require.Equal(t, []byte("third"), result.result.ReceiptBytes)

	resp = make(chan readReceiptResp, 1)
	coord.handleReadByTxHashInBlock(readByTxHashInBlockReq{
		ctx:         context.Background(),
		txHash:      txHash,
		blockNumber: 10,
		resp:        resp,
	})
	result = <-resp
	require.NoError(t, result.err)
	require.Equal(t, []byte("first"), result.result.ReceiptBytes)
}
