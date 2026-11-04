package blockstore

import (
	"encoding/binary"
	"strconv"
	"testing"

	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneBlockRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(1)
	for range 16 {
		blk := types.GenBlock(rng)
		value := types.BlockConv.Marshal(blk)
		decoded, err := types.BlockConv.Unmarshal(value)
		require.NoError(t, err)
		// Header hash uniquely identifies a block; equal hash => same block.
		require.Equal(t, blk.Header().Hash(), decoded.Header().Hash())
		// Re-encoding the decoded block must reproduce the same bytes.
		require.Equal(t, value, types.BlockConv.Marshal(decoded))
	}
}

func TestQCRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(2)
	for range 16 {
		qc := types.GenFullCommitQC(rng)
		value := types.FullCommitQCConv.Marshal(qc)
		decoded, err := types.FullCommitQCConv.Unmarshal(value)
		require.NoError(t, err)
		// Re-encoding the decoded QC must reproduce the same bytes.
		require.Equal(t, value, types.FullCommitQCConv.Marshal(decoded))
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	// Invalid bytes must surface an error rather than a partial value.
	garbage := []byte{0xff, 0xff, 0xff, 0xff}
	_, blockErr := types.BlockConv.Unmarshal(garbage)
	require.Error(t, blockErr)
	_, qcErr := types.FullCommitQCConv.Unmarshal(garbage)
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
			_, err := decodeBlock(record.Value)
			require.NoError(t, err)
			switch field {
			case "index":
				record.Value[7]++
			case "hash":
				record.Value[8] ^= 1
			}
			_, err = decodeBlock(record.Value)
			require.ErrorIs(t, err, types.ErrQCBlocksMismatch)
		})
	}
}

func TestBlockEncodingMatchesPersistedLayout(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			rng := utils.TestRng()
			lanes := make([]*types.Block, count)
			headers := make([]*types.BlockHeader, count)
			for i := range lanes {
				lanes[i] = types.GenBlock(rng)
				headers[i] = lanes[i].Header()
			}
			qc := types.GenFullCommitQCAt(rng, 7, count)
			qc = types.NewFullCommitQC(qc.QC(), headers)
			block := types.NewRoadBlock(qc, lanes)
			hash := block.Header().Hash()
			want := record{Key: (blockKey{Index: qc.Index()}).encode()}
			want.Value = binary.BigEndian.AppendUint64(want.Value, uint64(qc.Index()))
			want.Value = append(want.Value, hash[:]...)
			appendValue := func(value []byte, key ledgerKey) {
				want.Value = binary.BigEndian.AppendUint32(want.Value, uint32(len(value)))
				offset := uint32(len(want.Value))
				want.Value = append(want.Value, value...)
				want.Aliases = append(want.Aliases, &litttypes.SecondaryKey{Key: key.bytes(), Offset: offset, Length: uint32(len(value))})
			}
			appendValue(types.FullCommitQCConv.Marshal(qc), (qcKey{Index: qc.Index()}).encode())
			for i, lane := range lanes {
				appendValue(types.BlockConv.Marshal(lane), (laneBlockKey{Index: qc.Index(), Offset: uint64(i)}).encode())
			}
			want.Aliases = append(want.Aliases, &litttypes.SecondaryKey{Key: (blockHashKey{Hash: hash}).encode().bytes(), Length: uint32(len(want.Value))})
			got := encodeBlock(block)
			require.Equal(t, want, got)
			decoded, err := decodeBlock(got.Value)
			require.NoError(t, err)
			require.Equal(t, block.Header().Hash(), decoded.Header().Hash())
			require.Equal(t, got, encodeBlock(decoded))
		})
	}
}
