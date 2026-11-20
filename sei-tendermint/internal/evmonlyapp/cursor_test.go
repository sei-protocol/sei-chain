package evmonlyapp

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
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
	require.Equal(t, evmOnlyCursorHeaderSize+3*32, len(raw))
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

// A cursor holds as many base fees as the lag it was written under needs, from the fewest to the
// most any lag needs.
func TestEVMOnlyCursorRoundTripsEveryLag(t *testing.T) {
	for _, fees := range []int{2, int(chainparams.MaxBaseFeeLag) + 1} {
		cursor := evmOnlyCursor{height: 1, baseFees: make(evmOnlyBaseFees, fees)}
		for i := range cursor.baseFees {
			cursor.baseFees[i].SetUint64(uint64(i) + 1) //nolint:gosec // G115: i is a small index.
		}

		decoded, err := decodeEVMOnlyCursor(cursor.encode())

		require.NoError(t, err)
		require.Equal(t, cursor, decoded)
	}
}

func TestEVMOnlyCursorDecodeRejectsMalformed(t *testing.T) {
	valid := evmOnlyCursor{height: 1, baseFees: make(evmOnlyBaseFees, 3)}.encode()
	negativeHeight := make([]byte, len(valid))
	binary.BigEndian.PutUint64(negativeHeight, math.MaxUint64)
	for name, raw := range map[string][]byte{
		"empty":                nil,
		"header only":          valid[:evmOnlyCursorHeaderSize],
		"truncated":            valid[:len(valid)-1],
		"oversized":            append(append([]byte(nil), valid...), 0),
		"one base fee":         valid[:evmOnlyCursorHeaderSize+32],
		"too many base fees":   make([]byte, evmOnlyCursorHeaderSize+(int(chainparams.MaxBaseFeeLag)+2)*32),
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
	rules := genesisBaseFeeRules()
	const gasUsed = 21_000_000
	const gasLimit = 30_000_000

	next := cursor.nextBaseFees(rules, gasUsed, gasLimit)

	require.Equal(t, evmOnlyBaseFees{
		cursor.baseFees[1],
		cursor.baseFees[2],
		rules.step.NextBaseFee(cursor.baseFees[2], gasUsed, gasLimit),
	}, next)
	require.Equal(t, cursor.baseFees[0].ToBig(), cursor.baseFee())
	require.Equal(t, cursor.baseFees[1].ToBig(), cursor.nextBaseFee())
	require.Equal(t, cursor.baseFees[2].ToBig(), cursor.highestScheduledBaseFee())
}

// An update that would set a base fee below the minimum fee sets the minimum instead.
func TestEVMOnlyCursorNextBaseFeesStopsAtTheMinimumFee(t *testing.T) {
	rules := genesisBaseFeeRules()
	minimum := rules.step.MinBaseFee
	aboveMin := *new(uint256.Int).AddUint64(&minimum, 1)
	cursor := evmOnlyCursor{baseFees: evmOnlyBaseFees{aboveMin, aboveMin, aboveMin}}
	const gasLimit = 30_000_000
	unfloored := rules.step
	unfloored.MinBaseFee.Clear()
	lowered := unfloored.NextBaseFee(aboveMin, 0, gasLimit)
	require.True(t, lowered.Lt(&minimum))

	next := cursor.nextBaseFees(rules, 0, gasLimit)

	require.Equal(t, evmOnlyBaseFees{aboveMin, aboveMin, minimum}, next)
}

// A raised lag holds the last base fee set for each block no gas use sets, then sets the one the
// next block's gas use sets, now the raised lag after it.
func TestEVMOnlyCursorNextBaseFeesFillsARaisedLag(t *testing.T) {
	a, b, c := *uint256.NewInt(110_000_000_000), *uint256.NewInt(120_000_000_000), *uint256.NewInt(130_000_000_000)
	cursor := evmOnlyCursor{baseFees: evmOnlyBaseFees{a, b, c}}
	rules := genesisBaseFeeRules()
	rules.lag = 4
	const gasUsed = 21_000_000
	const gasLimit = 30_000_000

	next := cursor.nextBaseFees(rules, gasUsed, gasLimit)

	require.Equal(t, evmOnlyBaseFees{b, c, c, c, rules.step.NextBaseFee(c, gasUsed, gasLimit)}, next)

	// A held base fee is never below the minimum fee, and the one the gas use sets moves from it.
	minimum := *uint256.NewInt(140_000_000_000)
	rules.step.MinBaseFee = minimum
	next = cursor.nextBaseFees(rules, gasLimit, gasLimit)
	require.Equal(t, evmOnlyBaseFees{b, c, minimum, minimum, rules.step.NextBaseFee(minimum, gasLimit, gasLimit)}, next)
	require.True(t, minimum.Lt(&next[4]), "a full block raises the base fee above the held minimum")
}

// A lowered lag sets no base fee, ignoring the gas use, until the ones already set cover fewer
// blocks than the lag, so none of them changes.
func TestEVMOnlyCursorNextBaseFeesRunsDownALoweredLag(t *testing.T) {
	fees := evmOnlyBaseFees{
		*uint256.NewInt(110_000_000_000), *uint256.NewInt(120_000_000_000), *uint256.NewInt(130_000_000_000),
		*uint256.NewInt(140_000_000_000), *uint256.NewInt(150_000_000_000),
	}
	cursor := evmOnlyCursor{baseFees: fees}
	rules := genesisBaseFeeRules()
	rules.lag = 2
	const gasLimit = 30_000_000

	cursor.baseFees = cursor.nextBaseFees(rules, gasLimit, gasLimit)
	require.Equal(t, fees[1:], cursor.baseFees)
	cursor.baseFees = cursor.nextBaseFees(rules, gasLimit, gasLimit)
	require.Equal(t, fees[2:], cursor.baseFees)
	const gasUsed = 21_000_000
	cursor.baseFees = cursor.nextBaseFees(rules, gasUsed, gasLimit)
	require.Equal(t, evmOnlyBaseFees{fees[3], fees[4], rules.step.NextBaseFee(fees[4], gasUsed, gasLimit)}, cursor.baseFees)
}
