package blockstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestBlockRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(1)
	for i := range 16 {
		n := types.GlobalBlockNumber(i)
		blk := types.GenBlock(rng)
		value := encodeBlock(n, blk)
		require.Equal(t, blockSerializationVersion, value[0], "value must be version-prefixed")
		gotN, decoded, err := decodeBlock(value)
		require.NoError(t, err)
		// The embedded block number must round-trip.
		require.Equal(t, n, gotN)
		// Header hash uniquely identifies a block; equal hash => same block.
		require.Equal(t, blk.Header().Hash(), decoded.Header().Hash())
		// Re-encoding the decoded block (with the same number) must reproduce the same bytes.
		require.Equal(t, value, encodeBlock(gotN, decoded))
	}
}

func TestQCRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(2)
	for range 16 {
		qc := types.GenFullCommitQC(rng)
		value := encodeQC(qc)
		require.Equal(t, qcSerializationVersion, value[0], "value must be version-prefixed")
		decoded, err := decodeQC(value)
		require.NoError(t, err)
		// Re-encoding the decoded QC must reproduce the same bytes.
		require.Equal(t, value, encodeQC(decoded))
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	// Invalid bytes must surface an error rather than a partial value.
	garbage := []byte{0xff, 0xff, 0xff, 0xff}
	_, _, blockErr := decodeBlock(garbage)
	require.Error(t, blockErr)
	_, qcErr := decodeQC(garbage)
	require.Error(t, qcErr)
}

func TestDecodeRejectsUnknownVersion(t *testing.T) {
	rng := utils.TestRngFromSeed(3)

	// A block value whose version byte is not the current one must be rejected,
	// even though the rest of the value is well-formed.
	blockValue := encodeBlock(1, types.GenBlock(rng))
	blockValue[0] = blockSerializationVersion + 1
	_, _, blockErr := decodeBlock(blockValue)
	require.Error(t, blockErr)

	qcValue := encodeQC(types.GenFullCommitQC(rng))
	qcValue[0] = qcSerializationVersion + 1
	_, qcErr := decodeQC(qcValue)
	require.Error(t, qcErr)
}
