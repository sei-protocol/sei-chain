package keeper

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestConvertEthLog(t *testing.T) {
	// Create a sample ethtypes.Log object
	ethLog := &types.Log{
		Address: common.HexToAddress("0x123"),
		Topics: []common.Hash{
			common.HexToHash("0x456"),
			common.HexToHash("0x789"),
		},
		Data:        []byte("data"),
		BlockNumber: 1,
		TxHash:      common.HexToHash("0xabc"),
		TxIndex:     2,
		Index:       3,
	}

	// Convert the ethtypes.Log to a types.Log
	log := ConvertEthLog(ethLog)

	// Check that the fields match
	require.Equal(t, ethLog.Address.Hex(), log.Address)
	require.Equal(t, len(ethLog.Topics), len(log.Topics))
	for i, topic := range ethLog.Topics {
		require.Equal(t, topic.Hex(), log.Topics[i])
	}
	require.Equal(t, ethLog.Data, log.Data)
	require.Equal(t, uint32(ethLog.Index), log.Index)
}

func TestGetLogsForTx(t *testing.T) {
	// Create a sample types.Receipt object with a list of types.Log objects
	receipt := &evmtypes.Receipt{
		Logs: []*evmtypes.Log{
			{
				Address: common.HexToAddress("0x123").Hex(),
				Topics: []string{
					"0x0000000000000000000000000000000000000000000000000000000000000456",
					"0x0000000000000000000000000000000000000000000000000000000000000789",
				},
				Data:  []byte("data"),
				Index: 3,
			},
			{
				Address: common.HexToAddress("0x123").Hex(),
				Topics: []string{
					"0x0000000000000000000000000000000000000000000000000000000000000def",
					"0x0000000000000000000000000000000000000000000000000000000000000123",
				},
				Data:  []byte("data2"),
				Index: 4,
			},
		},
		BlockNumber:      1,
		TransactionIndex: 2,
		TxHashHex:        "0xabc",
	}

	// Convert the types.Receipt to a list of ethtypes.Log objects
	logs := GetLogsForTx(receipt)

	// Check that the fields match
	require.Equal(t, len(receipt.Logs), len(logs))
	for i, log := range logs {
		require.Equal(t, receipt.Logs[i].Address, log.Address.Hex())
		require.Equal(t, len(receipt.Logs[i].Topics), len(log.Topics))
		for j, topic := range log.Topics {
			require.Equal(t, receipt.Logs[i].Topics[j], topic.Hex())
		}
		require.Equal(t, receipt.Logs[i].Data, log.Data)
		require.Equal(t, uint(receipt.Logs[i].Index), log.Index)
	}
}
