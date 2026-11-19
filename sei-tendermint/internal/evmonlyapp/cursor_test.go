package evmonlyapp

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEVMOnlyCursorRoundTrip(t *testing.T) {
	cursor := evmOnlyCursor{
		height:    0x0102030405060708,
		appHash:   common.HexToHash("0xaa"),
		blockHash: common.HexToHash("0xbb"),
		gasLimit:  0x1112131415161718,
		baseFees:  evmOnlyBaseFees{*uint256.NewInt(0x21), *uint256.NewInt(0x22), *uint256.NewInt(0x23)},
	}
	raw := cursor.encode()
	require.Equal(t, evmOnlyCursorSize, len(raw))
	require.Equal(t, uint64(cursor.height), binary.BigEndian.Uint64(raw[:8])) //nolint:gosec // G115: fixture height is non-negative.
	require.Equal(t, cursor.appHash, common.BytesToHash(raw[8:40]))
	require.Equal(t, cursor.blockHash, common.BytesToHash(raw[40:72]))
	require.Equal(t, cursor.gasLimit, binary.BigEndian.Uint64(raw[72:80]))
	for i, fee := range cursor.baseFees {
		require.Equal(t, fee.Bytes32(), [32]byte(raw[80+32*i:80+32*(i+1)]))
	}

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

// The next block's cursor keeps the base fees this one set, and appends the one the next block's
// gas use sets. Gas used at height h sets the base fee of height h+2.
func TestEVMOnlyCursorNextBaseFeesLagsGasUseByTwoBlocks(t *testing.T) {
	cursor := evmOnlyCursor{
		baseFees: evmOnlyBaseFees{
			*uint256.NewInt(110_000_000_000), *uint256.NewInt(120_000_000_000), *uint256.NewInt(130_000_000_000),
		},
	}
	params := genesisBaseFeeParams()
	const gasUsed = 21_000_000
	const gasLimit = 30_000_000

	next := cursor.nextBaseFees(params, gasUsed, gasLimit)

	require.Equal(t, evmOnlyBaseFees{
		cursor.baseFees[1],
		cursor.baseFees[2],
		params.NextBaseFee(cursor.baseFees[2], gasUsed, gasLimit),
	}, next)
	require.Equal(t, cursor.baseFees[0].ToBig(), cursor.baseFee())
	require.Equal(t, cursor.baseFees[1].ToBig(), cursor.nextBaseFee())
	require.Equal(t, cursor.baseFees[2].ToBig(), cursor.highestScheduledBaseFee())
}
