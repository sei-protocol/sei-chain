package evmonlyapp

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEVMOnlyCursorRoundTrip(t *testing.T) {
	cursor := evmOnlyCursor{
		height:    0x0102030405060708,
		appHash:   common.HexToHash("0xaa"),
		blockHash: common.HexToHash("0xbb"),
		gasLimit:  0x1112131415161718,
	}
	raw := cursor.encode()
	require.Equal(t, evmOnlyCursorSize, len(raw))
	require.Equal(t, uint64(cursor.height), binary.BigEndian.Uint64(raw[:8])) //nolint:gosec // G115: fixture height is non-negative.
	require.Equal(t, cursor.appHash, common.BytesToHash(raw[8:40]))
	require.Equal(t, cursor.blockHash, common.BytesToHash(raw[40:72]))
	require.Equal(t, cursor.gasLimit, binary.BigEndian.Uint64(raw[72:]))

	decoded, err := decodeEVMOnlyCursor(raw)
	require.NoError(t, err)
	require.Equal(t, cursor, decoded)
}

func TestEVMOnlyCursorDecodeRejectsMalformed(t *testing.T) {
	valid := evmOnlyCursor{height: 1}.encode()
	negativeHeight := make([]byte, evmOnlyCursorSize)
	binary.BigEndian.PutUint64(negativeHeight, math.MaxUint64)
	for name, raw := range map[string][]byte{
		"empty":                nil,
		"truncated":            valid[:evmOnlyCursorSize-1],
		"oversized":            append(append([]byte(nil), valid...), 0),
		"height exceeds int64": negativeHeight,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeEVMOnlyCursor(raw)
			require.Error(t, err)
		})
	}
}
