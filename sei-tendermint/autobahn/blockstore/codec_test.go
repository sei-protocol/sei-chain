package blockstore

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestLaneBlockRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(1)
	for range 16 {
		blk := types.GenBlock(rng)
		value := encodeLaneBlock(blk)
		decoded, err := decodeLaneBlock(value)
		require.NoError(t, err)
		// Header hash uniquely identifies a block; equal hash => same block.
		require.Equal(t, blk.Header().Hash(), decoded.Header().Hash())
		// Re-encoding the decoded block must reproduce the same bytes.
		require.Equal(t, value, encodeLaneBlock(decoded))
	}
}

func TestQCRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(2)
	for range 16 {
		qc := types.GenFullCommitQC(rng)
		value := encodeQC(qc)
		decoded, err := decodeQC(value)
		require.NoError(t, err)
		// Re-encoding the decoded QC must reproduce the same bytes.
		require.Equal(t, value, encodeQC(decoded))
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	// Invalid bytes must surface an error rather than a partial value.
	garbage := []byte{0xff, 0xff, 0xff, 0xff}
	_, blockErr := decodeLaneBlock(garbage)
	require.Error(t, blockErr)
	_, qcErr := decodeQC(garbage)
	require.Error(t, qcErr)
}

func TestDecodeBlockRejectsInconsistentMetadata(t *testing.T) {
	for _, field := range []string{"index", "hash"} {
		t.Run(field, func(t *testing.T) {
			rng := utils.TestRngFromSeed(3)
			lane := types.GenBlock(rng)
			qc := types.GenFullCommitQCAt(rng, 3, 1)
			qc = types.NewFullCommitQC(qc.QC(), []*types.BlockHeader{lane.Header()})
			record := encodeBlock(types.NewRoadBlock(qc, []*types.Block{lane}))
			_, err := decodeBlock(record)
			require.NoError(t, err)
			switch field {
			case "index":
				record.Index++
			case "hash":
				record.Hash[0] ^= 1
			}
			_, err = decodeBlock(record)
			require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
		})
	}
}
