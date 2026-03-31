package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bitutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
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
	log := keeper.ConvertEthLog(ethLog)

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
	logs := keeper.GetLogsForTx(receipt, 0)

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

	// non-zero starting index
	logs = keeper.GetLogsForTx(receipt, 5)
	require.Equal(t, len(receipt.Logs), len(logs))
	for i, log := range logs {
		require.Equal(t, uint(receipt.Logs[i].Index+5), log.Index)
	}
}

func TestLegacyBlockBloomCutoffHeight(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(123)
	require.Equal(t, int64(0), k.GetLegacyBlockBloomCutoffHeight(ctx))
	k.SetLegacyBlockBloomCutoffHeight(ctx)
	require.Equal(t, int64(123), k.GetLegacyBlockBloomCutoffHeight(ctx))
}

func TestSetEvmOnlyBlockBloom_OptimisedMatchesOriginal(t *testing.T) {
	tests := []struct {
		name   string
		blooms []types.Bloom
	}{
		{
			name:   "empty blooms",
			blooms: nil,
		},
		{
			name:   "single bloom all zeros",
			blooms: []types.Bloom{{}},
		},
		{
			name: "single bloom with bits set",
			blooms: func() []types.Bloom {
				var b types.Bloom
				b[0] = 0xAA
				b[100] = 0xFF
				b[types.BloomByteLength-1] = 0x01
				return []types.Bloom{b}
			}(),
		},
		{
			name: "multiple blooms",
			blooms: func() []types.Bloom {
				var b1, b2, b3 types.Bloom
				b1[0] = 0x0F
				b1[50] = 0xAB
				b2[0] = 0xF0
				b2[75] = 0xCD
				b3[200] = 0xFF
				return []types.Bloom{b1, b2, b3}
			}(),
		},
		{
			name: "overlapping bits",
			blooms: func() []types.Bloom {
				var b1, b2 types.Bloom
				b1[10] = 0b10101010
				b2[10] = 0b01010101
				return []types.Bloom{b1, b2}
			}(),
		},
		{
			name: "all ones",
			blooms: func() []types.Bloom {
				var b types.Bloom
				for i := range b {
					b[i] = 0xFF
				}
				return []types.Bloom{b}
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := setEvmOnlyBlockBloomOriginal(tc.blooms)
			optimised := keeper.BloomsToBytes(tc.blooms)

			if len(original) != len(optimised) {
				t.Fatalf("length mismatch: original=%d optimised=%d", len(original), len(optimised))
			}

			for i := range original {
				if original[i] != optimised[i] {
					t.Fatalf("mismatch at byte %d: original=0x%02X optimised=0x%02X", i, original[i], optimised[i])
				}
			}
		})
	}
}

func BenchmarkSetEvmOnlyBlockBloom_Original(b *testing.B) {
	blooms := makeBenchBlooms()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		setEvmOnlyBlockBloomOriginal(blooms)
	}
}

func BenchmarkSetEvmOnlyBlockBloom_Optimised(b *testing.B) {
	blooms := makeBenchBlooms()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keeper.BloomsToBytes(blooms)
	}
}

func makeBenchBlooms() []types.Bloom {
	blooms := make([]types.Bloom, 50)
	for i := range blooms {
		for j := range blooms[i] {
			blooms[i][j] = byte((i * j) & 0xFF)
		}
	}
	return blooms
}

func setEvmOnlyBlockBloomOriginal(blooms []types.Bloom) []byte {
	// The original implementation left for benchmark comparison.
	blockBloom := make([]byte, types.BloomByteLength)
	for _, bloom := range blooms {
		or := make([]byte, types.BloomByteLength)
		bitutil.ORBytes(or, blockBloom, bloom[:])
		blockBloom = or
	}
	return blockBloom
}
