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
		height:     0x0102030405060708,
		appHash:    common.HexToHash("0xaa"),
		blockHash:  common.HexToHash("0xbb"),
		prevRandao: common.HexToHash("0xcc"),
		gasLimit:   0x1112131415161718,
	}
	raw := cursor.encode()
	require.Equal(t, evmOnlyCursorSize, len(raw))
	require.Equal(t, uint64(cursor.height), binary.BigEndian.Uint64(raw[:8])) //nolint:gosec // G115: fixture height is non-negative.
	require.Equal(t, cursor.appHash, common.BytesToHash(raw[8:40]))
	require.Equal(t, cursor.blockHash, common.BytesToHash(raw[40:72]))
	require.Equal(t, cursor.prevRandao, common.BytesToHash(raw[72:104]))
	require.Equal(t, cursor.gasLimit, binary.BigEndian.Uint64(raw[104:]))

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
		"under legacy width":   make([]byte, evmOnlyCursorLegacySize-1),
		"over legacy width":    make([]byte, evmOnlyCursorLegacySize+1),
		"height exceeds int64": negativeHeight,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeEVMOnlyCursor(raw)
			require.Error(t, err)
		})
	}
}

// encodeLegacyEVMOnlyCursor writes a cursor the way the binary did before
// prevRandao existed: height, appHash, blockHash, gasLimit, with no prevRandao.
func encodeLegacyEVMOnlyCursor(height int64, appHash, blockHash common.Hash, gasLimit uint64) []byte {
	buf := make([]byte, 0, evmOnlyCursorLegacySize)
	buf = binary.BigEndian.AppendUint64(buf, uint64(height)) //nolint:gosec // test input is non-negative.
	buf = append(buf, appHash[:]...)
	buf = append(buf, blockHash[:]...)
	return binary.BigEndian.AppendUint64(buf, gasLimit)
}

// TestEVMOnlyCursorDecodesLegacyWidth covers a node upgrading across the commit
// that widened the cursor. Its on-disk cursor is the older width, and without the
// migration the node refuses to start rather than resuming.
func TestEVMOnlyCursorDecodesLegacyWidth(t *testing.T) {
	appHash := common.HexToHash("0xaa11")
	blockHash := common.HexToHash("0xbb22")
	const height = int64(296796)
	const gasLimit = uint64(0x0102030405060708)

	raw := encodeLegacyEVMOnlyCursor(height, appHash, blockHash, gasLimit)
	require.Equal(t, evmOnlyCursorLegacySize, len(raw))

	cursor, err := decodeEVMOnlyCursor(raw)
	require.NoError(t, err)
	require.Equal(t, height, cursor.height)
	require.Equal(t, appHash, cursor.appHash)
	require.Equal(t, blockHash, cursor.blockHash)
	// The field the older width did not carry reads zero, identically on every node.
	require.Equal(t, common.Hash{}, cursor.prevRandao)
	// prevRandao is spliced in ahead of the gas limit rather than appended. Appending
	// would take the gas limit from the zeroed bytes and read this value as prevRandao,
	// so this assertion is what separates the two.
	require.Equal(t, gasLimit, cursor.gasLimit)
}
