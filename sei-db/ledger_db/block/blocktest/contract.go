// Package blocktest holds the BlockDB contract suite and opaque record fixtures.
package blocktest

import (
	"bytes"
	"fmt"
	"testing"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/stretchr/testify/require"
)

// Open opens a handle to the same backing store, including across restart.
type Open func() (blocktypes.BlockDB, error)

// Builder returns an Open bound to a fresh store for one subtest.
type Builder func(t *testing.T) Open

// RunContract exercises record lookups, atomicity, recovery, and retention across backends.
func RunContract(t *testing.T, build Builder) {
	t.Run("EmptyStore", func(t *testing.T) {
		db, _ := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		record, exists, err := db.GetBlockByNumber(0)
		require.NoError(t, err)
		require.False(t, exists)
		require.Equal(t, blocktypes.BlockRecord{}, record)
		_, exists, err = db.GetBlockByHash(BlockHash(0))
		require.NoError(t, err)
		require.False(t, exists)
		for _, get := range []func(blocktypes.BlockNumber) ([]byte, bool, error){db.GetQC, db.GetAppQC, db.GetAppProposal} {
			_, exists, err := get(0)
			require.NoError(t, err)
			require.False(t, exists)
		}
		_, exists, err = db.GetLaneBlock(0, 0)
		require.NoError(t, err)
		require.False(t, exists)
		require.Equal(t, blocktypes.Suffix{}, readSuffix(t, db))
		require.Zero(t, db.GetPruneWatermark())
	})
	t.Run("TypedRecordRoundTrip", func(t *testing.T) {
		db, reopen := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		records := []blocktypes.BlockRecord{
			{Index: 3, Hash: BlockHash(3), QC: []byte("opaque QC"), LaneBlocks: [][]byte{[]byte("lane 0"), {}, []byte("lane 2")}},
			{Index: 4, Hash: BlockHash(4), QC: []byte("empty SuperBlock QC")},
		}
		for _, record := range records {
			require.NoError(t, db.PutBlock(record))
		}
		proposal := blocktypes.AppProposalRecord{Index: 3, Data: []byte("opaque AppProposal")}
		qc := blocktypes.AppQCRecord{Index: 3, Data: []byte("opaque AppQC")}
		require.NoError(t, db.PutAppProposal(proposal))
		require.NoError(t, db.PutAppQC(qc))
		db = restart(t, reopen, db)
		for _, record := range records {
			assertRecord(t, db, record)
		}
		gotProposal, exists, err := db.GetAppProposal(3)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, proposal.Data, gotProposal)
		gotQC, exists, err := db.GetAppQC(3)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, qc.Data, gotQC)
		for _, get := range []func(blocktypes.BlockNumber) ([]byte, bool, error){db.GetAppQC, db.GetAppProposal} {
			_, exists, err := get(4)
			require.NoError(t, err)
			require.False(t, exists, "app records occupy only their own index")
		}
		require.Equal(t, blocktypes.Suffix{Blocks: records, AppQCs: []blocktypes.AppQCRecord{qc}, AppProposals: []blocktypes.AppProposalRecord{proposal}}, readSuffix(t, db))
	})
	t.Run("WriteCopiesInput", func(t *testing.T) {
		db, _ := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		record := BlockRecord(7, 2)
		want := BlockRecord(7, 2)
		require.NoError(t, db.PutBlock(record))
		record.Hash[0] ^= 1
		record.QC[0] ^= 1
		record.LaneBlocks[0][0] ^= 1
		record.LaneBlocks[1] = []byte("changed")
		assertRecord(t, db, want)
		proposal := blocktypes.AppProposalRecord{Index: 7, Data: []byte("proposal")}
		appQC := blocktypes.AppQCRecord{Index: 7, Data: []byte("appQC")}
		require.NoError(t, db.PutAppProposal(proposal))
		require.NoError(t, db.PutAppQC(appQC))
		proposal.Data[0] ^= 1
		appQC.Data[0] ^= 1
		got, _, err := db.GetAppProposal(7)
		require.NoError(t, err)
		require.Equal(t, []byte("proposal"), got)
		got, _, err = db.GetAppQC(7)
		require.NoError(t, err)
		require.Equal(t, []byte("appQC"), got)
	})
	t.Run("ReadSuffixIsASnapshot", func(t *testing.T) {
		db, _ := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		require.NoError(t, db.PutBlock(BlockRecord(0, 2)))
		before := readSuffix(t, db)
		require.NoError(t, db.PutBlock(BlockRecord(1, 3)))
		db.SetPruneWatermark(1)
		require.Equal(t, []blocktypes.BlockRecord{BlockRecord(0, 2)}, before.Blocks)
		require.Equal(t, BlockRecord(1, 3), readSuffix(t, db).Blocks[len(readSuffix(t, db).Blocks)-1])
	})
	t.Run("ReadSuffixStartsAtLatestAppQC", func(t *testing.T) {
		db, _ := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		WriteCohorts(t, db, 5, 3)
		PutAppData(t, db, 0)
		PutAppData(t, db, 1)
		require.NoError(t, db.PutAppProposal(blocktypes.AppProposalRecord{Index: 2, Data: RecordValue("app_proposal", 2)}))
		suffix := readSuffix(t, db)
		require.Len(t, suffix.Blocks, 4)
		for i, record := range suffix.Blocks {
			require.Equal(t, blocktypes.BlockNumber(i+1), record.Index)
		}
		require.Equal(t, []blocktypes.AppQCRecord{{Index: 1, Data: RecordValue("app_qc", 1)}}, suffix.AppQCs)
		require.Equal(t, []blocktypes.AppProposalRecord{{Index: 1, Data: RecordValue("app_proposal", 1)}, {Index: 2, Data: RecordValue("app_proposal", 2)}}, suffix.AppProposals)
	})
	t.Run("RestartPersistsRecords", func(t *testing.T) {
		db, reopen := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		WriteCohorts(t, db, 4, 5)
		before := readSuffix(t, db)
		db = restart(t, reopen, db)
		require.Equal(t, before, readSuffix(t, db))
		for _, record := range before.Blocks {
			assertRecord(t, db, record)
		}
	})
	t.Run("WatermarkIsMonotonic", func(t *testing.T) {
		db, _ := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		WriteCohorts(t, db, 4, 5)
		for _, n := range []blocktypes.BlockNumber{2, 1, 2, 1000} {
			previous := db.GetPruneWatermark()
			db.SetPruneWatermark(n)
			require.Equal(t, max(previous, n), db.GetPruneWatermark())
		}
	})
	t.Run("AtomicBlock", func(t *testing.T) {
		db, reopen := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		for index := range blocktypes.BlockNumber(3) {
			require.NoError(t, db.PutBlock(BlockRecord(index, int(index))))
		}
		db = restart(t, reopen, db)
		for index := range blocktypes.BlockNumber(3) {
			assertRecord(t, db, BlockRecord(index, int(index)))
		}
		_, exists, err := db.GetBlockByHash(BlockHash(100))
		require.NoError(t, err)
		require.False(t, exists)
	})
}

func assertRecord(t *testing.T, db blocktypes.BlockDB, want blocktypes.BlockRecord) {
	t.Helper()
	got, exists, err := db.GetBlockByNumber(want.Index)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, want, got)
	byHash, exists, err := db.GetBlockByHash(want.Hash)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, want, byHash)
	qc, exists, err := db.GetQC(want.Index)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, want.QC, qc)
	for offset, lane := range want.LaneBlocks {
		got, exists, err := db.GetLaneBlock(want.Index, uint64(offset))
		require.NoError(t, err)
		require.True(t, exists)
		require.True(t, bytes.Equal(lane, got))
	}
	_, exists, err = db.GetLaneBlock(want.Index, uint64(len(want.LaneBlocks)))
	require.NoError(t, err)
	require.False(t, exists)
}

func openFresh(t *testing.T, build Builder) (blocktypes.BlockDB, Open) {
	t.Helper()
	reopen := build(t)
	db, err := reopen()
	require.NoError(t, err)
	return db, reopen
}

func restart(t *testing.T, reopen Open, db blocktypes.BlockDB) blocktypes.BlockDB {
	t.Helper()
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	next, err := reopen()
	require.NoError(t, err)
	return next
}

func readSuffix(t *testing.T, db blocktypes.BlockDB) blocktypes.Suffix {
	t.Helper()
	suffix, err := db.ReadSuffix()
	require.NoError(t, err)
	return suffix
}

// valueSize is the padded fixture width used by torn-write tests.
const valueSize = 64

// BlockHash returns a unique opaque hash for road n.
func BlockHash(n blocktypes.BlockNumber) blocktypes.BlockHash {
	return blocktypes.BlockHash(fmt.Sprintf("block-hash-%020d", n))
}

// RecordValue returns a distinct opaque payload for kind at n.
func RecordValue(kind string, n blocktypes.BlockNumber) []byte {
	value := []byte(fmt.Sprintf("%s@%d", kind, n))
	return append(value, bytes.Repeat([]byte("."), max(0, valueSize-len(value)))...)
}

// BlockRecord returns an opaque SuperBlock fixture with the requested lane-block count.
func BlockRecord(index blocktypes.BlockNumber, lanes int) blocktypes.BlockRecord {
	record := blocktypes.BlockRecord{Index: index, Hash: BlockHash(index), QC: RecordValue("qc", index)}
	for offset := range lanes {
		record.LaneBlocks = append(record.LaneBlocks, RecordValue("block", index*blocktypes.BlockNumber(lanes)+blocktypes.BlockNumber(offset)))
	}
	return record
}

// PutAppData stores one application proposal and its QC at index.
func PutAppData(t *testing.T, db blocktypes.BlockDB, index blocktypes.BlockNumber) {
	t.Helper()
	require.NoError(t, db.PutAppProposal(blocktypes.AppProposalRecord{Index: index, Data: RecordValue("app_proposal", index)}))
	require.NoError(t, db.PutAppQC(blocktypes.AppQCRecord{Index: index, Data: RecordValue("app_qc", index)}))
}

// WriteCohorts stores consecutive SuperBlocks from road 0, with perCohort lane blocks each.
func WriteCohorts(t *testing.T, db blocktypes.BlockDB, cohorts, perCohort uint64) {
	t.Helper()
	for i := range cohorts {
		require.NoError(t, db.PutBlock(BlockRecord(blocktypes.BlockNumber(i), int(perCohort)))) //nolint:gosec // small fixture sizes
	}
}

// WriteAppData stores app data for the SuperBlocks written by WriteCohorts.
func WriteAppData(t *testing.T, db blocktypes.BlockDB, cohorts uint64) {
	t.Helper()
	for i := range cohorts {
		PutAppData(t, db, blocktypes.BlockNumber(i))
	}
}
