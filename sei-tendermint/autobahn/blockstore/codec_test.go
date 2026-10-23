package blockstore

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestBlockRoundTrip(t *testing.T) {
	rng := utils.TestRngFromSeed(1)
	for range 16 {
		blk := types.GenBlock(rng)
		value := encodeBlock(blk)
		decoded, err := decodeBlock(value)
		require.NoError(t, err)
		// Header hash uniquely identifies a block; equal hash => same block.
		require.Equal(t, blk.Header().Hash(), decoded.Header().Hash())
		// Re-encoding the decoded block must reproduce the same bytes.
		require.Equal(t, value, encodeBlock(decoded))
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
	_, blockErr := decodeBlock(garbage)
	require.Error(t, blockErr)
	_, qcErr := decodeQC(garbage)
	require.Error(t, qcErr)
}

func TestDecodeRejectsUnknownVersion(t *testing.T) {
	t.Run("QCGroup", func(t *testing.T) {
		rng := utils.TestRng()
		block := types.GenBlock(rng)
		qc := types.GenFullCommitQCRange(rng, 1, 2)
		qc = types.NewFullCommitQC(qc.QC(), []*types.BlockHeader{block.Header()})
		db := memblock.NewBlockDB()
		err := db.PutQCGroup(blocktypes.QCGroupRecord{Version: blocktypes.CurrentRecordVersion, Data: encodeQC(qc), Blocks: []blocktypes.BlockRecord{
			{Number: 1, Hash: block.Header().Hash().Bytes(), Value: encodeBlock(block)},
		}})
		require.NoError(t, err)
		value, present, err := db.GetQC(1)
		require.NoError(t, err)
		require.True(t, present)
		decoded, err := decodeQCRecord(value)
		require.NoError(t, err)
		require.Equal(t, qc.QC().GlobalRange(), decoded.QC().GlobalRange())

		// The group version governs both the QC and its blocks.
		value.Version++
		_, err = decodeQCRecord(value)
		require.Error(t, err)
	})
	t.Run("AppProposal", func(t *testing.T) {
		rng := utils.TestRng()
		db := memblock.NewBlockDB()
		require.NoError(t, db.PutAppProposal(blocktypes.AppProposalRecord{
			Version: blocktypes.CurrentRecordVersion, Data: encodeAppProposal(types.GenAppProposal(rng)), First: 1, Next: 2,
		}))
		value, present, err := db.GetAppProposal(1)
		require.NoError(t, err)
		require.True(t, present)
		_, err = decodeAppProposal(value)
		require.NoError(t, err)
		value.Version++
		_, err = decodeAppProposal(value)
		require.Error(t, err)
	})
	t.Run("AppQC", func(t *testing.T) {
		rng := utils.TestRng()
		db := memblock.NewBlockDB()
		require.NoError(t, db.PutAppQC(blocktypes.AppQCRecord{
			Version: blocktypes.CurrentRecordVersion, Data: encodeAppQC(types.GenAppQC(rng)), First: 1, Next: 2,
		}))
		value, present, err := db.GetAppQC(1)
		require.NoError(t, err)
		require.True(t, present)
		_, err = decodeAppQC(value)
		require.NoError(t, err)
		value.Version++
		_, err = decodeAppQC(value)
		require.Error(t, err)
	})
}
