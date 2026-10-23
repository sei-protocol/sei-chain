// Package blocktest holds the blocktypes.BlockDB contract suite and the record
// fixtures its backends test against.
//
// The suite is shared rather than per-backend: every backend owes the same
// observable behavior, and a copy per backend is a copy that drifts. It knows
// nothing of the consensus layer above — a BlockDB stores opaque bytes, so the
// fixtures store opaque bytes, and the ordering and encoding rules that give
// those bytes meaning are tested where they live.
package blocktest

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// Open opens a handle to the BlockDB under test. Calling it more than once
// reopens a handle to the SAME backing store, simulating a process restart:
// in-memory backends hand back the same instance, durable ones reopen their
// files. The caller must Close the previous handle before reopening.
type Open func() (blocktypes.BlockDB, error)

// Builder returns an Open bound to a fresh, empty backing store, for one subtest.
type Builder func(t *testing.T) Open

// RunContract exercises the blocktypes.BlockDB contract against the backing
// store build produces.
//
// Where the watermark is allowed to go is absent, because a BlockDB does not
// decide that: it moves the floor where it is told. So is physical reclamation —
// the contract makes eligibility observable through GetPruneWatermark but leaves the
// timing and granularity of the reclaim to the backend, so what a backend
// actually drops, and when, is covered in that backend's own package.
func RunContract(t *testing.T, build Builder) {
	t.Run("TypedRecordRoundTrip", func(t *testing.T) {
		db, reopen := openFresh(t, build)
		qc := blocktypes.QCGroupRecord{QC: blocktypes.QCRecord{Data: []byte("opaque QC")}, Blocks: []blocktypes.BlockRecord{
			{Number: 3, Hash: blocktypes.BlockHash("hash 3"), Value: []byte("opaque block 3")},
			{Number: 4, Hash: blocktypes.BlockHash("hash 4"), Value: []byte("opaque block 4")},
		}}
		appQC := blocktypes.AppQCRecord{Data: []byte("opaque AppQC"), First: 3, Next: 5}
		proposal := blocktypes.AppProposalRecord{Data: []byte("opaque AppProposal"), First: 3, Next: 5}
		require.NoError(t, db.PutQC(qc))
		require.NoError(t, db.PutAppProposal(proposal))
		require.NoError(t, db.PutAppQC(appQC))
		db = restart(t, reopen, db)
		defer func() { require.NoError(t, db.Close()) }()
		for _, block := range qc.Blocks {
			gotQC, ok, err := db.GetQC(block.Number)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, qc.QC.Data, gotQC)
			gotBlock, ok, err := db.GetBlock(block.Number)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, block.Value, gotBlock)
			number, gotByHash, ok, err := db.GetBlockByHash(block.Hash)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, block.Number, number)
			require.Equal(t, block.Value, gotByHash)
			gotAppQC, ok, err := db.GetAppQC(block.Number)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, appQC.Data, gotAppQC)
			gotProposal, ok, err := db.GetAppProposal(block.Number)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, proposal.Data, gotProposal)
		}
		suffix, err := db.ReadSuffix()
		require.NoError(t, err)
		wantBlocks := make([]blocktypes.BlockRecord, len(qc.Blocks))
		for i, block := range qc.Blocks {
			wantBlocks[i] = blocktypes.BlockRecord{Number: block.Number, Value: block.Value}
		}
		require.Equal(t, blocktypes.Suffix{QCs: []blocktypes.QCRecord{qc.QC}, Blocks: wantBlocks, AppQCs: []blocktypes.AppQCRecord{appQC}, AppProposals: []blocktypes.AppProposalRecord{proposal}}, suffix)
	})
	t.Run("AtomicQC", func(t *testing.T) { testAtomicQC(t, build) })
	t.Run("EmptyStore", func(t *testing.T) { testEmptyStore(t, build) })
	t.Run("RecordRoundTrip", func(t *testing.T) { testRecordRoundTrip(t, build) })
	t.Run("BlockHashAlias", func(t *testing.T) { testBlockHashAlias(t, build) })
	t.Run("ReadSuffixFollowsWriteOrder", func(t *testing.T) { testReadSuffixFollowsWriteOrder(t, build) })
	t.Run("ReadSuffixIsASnapshot", func(t *testing.T) { testReadSuffixIsASnapshot(t, build) })
	t.Run("ReadSuffixStartsAtLatestAppQC", func(t *testing.T) {
		db, _ := openFresh(t, build)
		defer func() { require.NoError(t, db.Close()) }()
		PutQCGroup(t, db, 0, 3)
		PutQCGroup(t, db, 3, 6)
		PutAppData(t, db, 0, 3)
		PutQCGroup(t, db, 6, 9)
		PutAppData(t, db, 3, 6)
		PutQCGroup(t, db, 9, 12)
		suffix := readSuffix(t, db)
		require.Equal(t, []blocktypes.QCRecord{
			{Data: RecordValue("qc", 3)},
			{Data: RecordValue("qc", 6)},
			{Data: RecordValue("qc", 9)},
		}, suffix.QCs)
		require.Len(t, suffix.Blocks, 9)
		for i, block := range suffix.Blocks {
			require.Equal(t, blocktypes.BlockNumber(i+3), block.Number)
		}
		require.Len(t, suffix.AppQCs, 1)
		require.Equal(t, blocktypes.BlockNumber(3), suffix.AppQCs[0].First)
		require.Len(t, suffix.AppProposals, 1)
		require.Equal(t, blocktypes.BlockNumber(3), suffix.AppProposals[0].First)

	})
	t.Run("RestartPersistsRecords", func(t *testing.T) { testRestartPersistsRecords(t, build) })
	t.Run("WatermarkIsMonotonic", func(t *testing.T) { testWatermarkIsMonotonic(t, build) })
}

// openFresh opens a handle to a new, empty backing store and returns it along
// with the Open that can reopen the same store (for restart).
func openFresh(t *testing.T, build Builder) (blocktypes.BlockDB, Open) {
	t.Helper()
	o := build(t)
	db, err := o()
	require.NoError(t, err)
	return db, o
}

// restart flushes and closes db, then reopens a handle to the same backing
// store. The returned handle must be closed by the caller.
func restart(t *testing.T, o Open, db blocktypes.BlockDB) blocktypes.BlockDB {
	t.Helper()
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	reopened, err := o()
	require.NoError(t, err)
	return reopened
}

// recordLookups exercises every numbered public lookup.
var recordLookups = []struct {
	name string
	get  func(blocktypes.BlockDB, blocktypes.BlockNumber) ([]byte, bool, error)
}{
	{"block", blocktypes.BlockDB.GetBlock},
	{"qc", blocktypes.BlockDB.GetQC},
	{"app_proposal", blocktypes.BlockDB.GetAppProposal},
	{"app_qc", blocktypes.BlockDB.GetAppQC},
}

func testAtomicQC(t *testing.T, build Builder) {
	db, reopen := openFresh(t, build)
	numbers := []blocktypes.BlockNumber{3, 5, 8}
	blocks := make([]blocktypes.BlockRecord, len(numbers))
	for i, n := range numbers {
		blocks[i] = blocktypes.BlockRecord{Number: n, Hash: BlockHash(n), Value: RecordValue("block", n)}
	}
	qc := blocktypes.QCGroupRecord{QC: blocktypes.QCRecord{Data: RecordValue("qc", 3)}}
	require.Panics(t, func() { _ = db.PutQC(qc) })
	require.Equal(t, blocktypes.Suffix{}, readSuffix(t, db))
	qc.Blocks = blocks
	require.NoError(t, db.PutQC(qc))
	later := make([]blocktypes.BlockRecord, len(blocks))
	for i, block := range blocks {
		later[i] = blocktypes.BlockRecord{Number: 10 + blocktypes.BlockNumber(i), Hash: BlockHash(10 + blocktypes.BlockNumber(i)), Value: block.Value}
	}
	laterQC := qc
	laterQC.Blocks = later
	require.NoError(t, db.PutQC(laterQC))
	suffix := readSuffix(t, db)
	require.Equal(t, []blocktypes.QCRecord{qc.QC, laterQC.QC}, suffix.QCs)
	wantBlocks := append(append([]blocktypes.BlockRecord{}, blocks...), later...)
	for i := range wantBlocks {
		wantBlocks[i].Hash = nil
	}
	require.Equal(t, wantBlocks, suffix.Blocks)
	for _, n := range []blocktypes.BlockNumber{3, 10} {
		got, present, err := db.GetQC(n)
		require.NoError(t, err)
		require.True(t, present)
		want := qc
		if n == 10 {
			want = laterQC
		}
		require.Equal(t, want.QC.Data, got)
	}
	before := readSuffix(t, db)
	db = restart(t, reopen, db)
	defer func() { require.NoError(t, db.Close()) }()
	require.Equal(t, before, readSuffix(t, db))
	// A floor inside a packed group retains the entire group.
	db.SetPruneWatermark(7)
	for _, block := range blocks {
		n := block.Number
		got, present, err := db.GetQC(n)
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, qc.QC.Data, got)
		got, present, err = db.GetBlock(n)
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, block.Value, got)
		number, byHash, present, err := db.GetBlockByHash(block.Hash)
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, block.Number, number)
		require.Equal(t, block.Value, byHash)
	}
	for _, n := range []blocktypes.BlockNumber{4, 6, 7} {
		for _, get := range []func(blocktypes.BlockNumber) ([]byte, bool, error){db.GetBlock, db.GetQC} {
			_, present, err := get(n)
			require.NoError(t, err)
			require.False(t, present, "only explicit block numbers should be indexed")
		}
	}
	// Reclaim the first group using its last explicit block number as the bound.
	db.SetPruneWatermark(9)
	for i, n := range numbers {
		_, qcPresent, err := db.GetQC(n)
		require.NoError(t, err)
		_, blockPresent, err := db.GetBlock(n)
		require.NoError(t, err)
		require.Equal(t, qcPresent, blockPresent)
		_, _, hashPresent, err := db.GetBlockByHash(blocks[i].Hash)
		require.NoError(t, err)
		require.Equal(t, blockPresent, hashPresent)
	}
}

func testEmptyStore(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	for _, lookup := range recordLookups {
		_, exists, err := lookup.get(db, 0)
		require.NoError(t, err)
		require.False(t, exists, "%s must be absent from an empty store", lookup.name)
	}
	_, _, exists, err := db.GetBlockByHash(BlockHash(0))
	require.NoError(t, err)
	require.False(t, exists, "no hash resolves in an empty store")

	require.Equal(t, blocktypes.Suffix{}, readSuffix(t, db))

	require.Equal(t, blocktypes.BlockNumber(0), db.GetPruneWatermark(), "nothing is eligible for reclamation yet")
}

func testRecordRoundTrip(t *testing.T, build Builder) {
	cases := []struct {
		name string
		get  func(blocktypes.BlockDB, blocktypes.BlockNumber) ([]byte, bool, error)
		put  func(blocktypes.BlockDB, []byte) error
	}{
		{"app_proposal", blocktypes.BlockDB.GetAppProposal, func(db blocktypes.BlockDB, data []byte) error {
			return db.PutAppProposal(blocktypes.AppProposalRecord{Data: data, First: 3, Next: 8})
		}},
		{"app_qc", blocktypes.BlockDB.GetAppQC, func(db blocktypes.BlockDB, data []byte) error {
			return db.PutAppQC(blocktypes.AppQCRecord{Data: data, First: 3, Next: 8})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openFresh(t, build)
			defer func() { require.NoError(t, db.Close()) }()
			want := RecordValue(tc.name, 3)
			require.NoError(t, tc.put(db, want))
			for n := blocktypes.BlockNumber(3); n < 8; n++ {
				value, exists, err := tc.get(db, n)
				require.NoError(t, err)
				require.True(t, exists)
				require.Equal(t, want, value)
			}
			for _, n := range []blocktypes.BlockNumber{2, 8} {
				_, exists, err := tc.get(db, n)
				require.NoError(t, err)
				require.False(t, exists)
			}
			for _, lookup := range recordLookups {
				if lookup.name == tc.name {
					continue
				}
				_, exists, err := lookup.get(db, 3)
				require.NoError(t, err)
				require.False(t, exists, "record lookups must have distinct keys")
			}
		})
	}
}

func testBlockHashAlias(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutQCGroup(t, db, 0, 3)

	for n := blocktypes.BlockNumber(0); n < 3; n++ {
		want := RecordValue("block", n)

		byNumber, exists, err := db.GetBlock(n)
		require.NoError(t, err)
		require.True(t, exists, "block %d must resolve by number", n)
		require.Equal(t, want, byNumber)

		number, byHash, exists, err := db.GetBlockByHash(BlockHash(n))
		require.NoError(t, err)
		require.True(t, exists, "block %d must resolve by hash", n)
		require.Equal(t, n, number)
		require.Equal(t, want, byHash, "both addresses reach the same value")
	}

	_, _, exists, err := db.GetBlockByHash(blocktypes.BlockHash("no-block-was-stored-under-this"))
	require.NoError(t, err)
	require.False(t, exists)
}

func testReadSuffixFollowsWriteOrder(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutQCGroup(t, db, 0, 3)
	PutAppData(t, db, 0, 3)

	suffix := readSuffix(t, db)
	require.Len(t, suffix.QCs, 1)
	require.Len(t, suffix.Blocks, 3)
	require.Len(t, suffix.AppQCs, 1)
	require.Len(t, suffix.AppProposals, 1)

}

func testReadSuffixIsASnapshot(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	PutQCGroup(t, db, 0, 2)

	before, err := db.ReadSuffix()
	require.NoError(t, err)
	PutQCGroup(t, db, 2, 4)
	require.Len(t, before.QCs, 1)
	require.Equal(t, blocktypes.BlockNumber(0), before.Blocks[0].Number)
	after, err := db.ReadSuffix()
	require.NoError(t, err)
	require.Len(t, after.QCs, 2)

}

func testRestartPersistsRecords(t *testing.T, build Builder) {
	db, o := openFresh(t, build)
	WriteCohorts(t, db, 2, 3)
	before := readSuffix(t, db)

	db = restart(t, o, db)
	defer func() { _ = db.Close() }()

	require.Equal(t, before, readSuffix(t, db), "a restart must not disturb the record log")
	for n := blocktypes.BlockNumber(0); n < 6; n++ {
		_, exists, err := db.GetBlock(n)
		require.NoError(t, err)
		require.True(t, exists, "block %d must survive the restart", n)

		_, _, exists, err = db.GetBlockByHash(BlockHash(n))
		require.NoError(t, err)
		require.True(t, exists, "block %d's hash alias must survive the restart", n)
	}
}

func testWatermarkIsMonotonic(t *testing.T, build Builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	WriteCohorts(t, db, 4, 5)

	db.SetPruneWatermark(10)
	require.Equal(t, blocktypes.BlockNumber(10), db.GetPruneWatermark(), "the floor moves where it is told")

	db.SetPruneWatermark(3)
	require.Equal(t, blocktypes.BlockNumber(10), db.GetPruneWatermark(), "the floor must never move backwards")

	db.SetPruneWatermark(10)
	require.Equal(t, blocktypes.BlockNumber(10), db.GetPruneWatermark(), "repeating a request changes nothing")

	// A floor above everything held is legal: the layer above is the one that caps
	// the request, and refusing it here would hide that it stopped doing so.
	db.SetPruneWatermark(1_000)
	require.Equal(t, blocktypes.BlockNumber(1_000), db.GetPruneWatermark())
}

// readSuffix reads the typed recovery suffix.
func readSuffix(t *testing.T, db blocktypes.BlockDB) blocktypes.Suffix {
	t.Helper()
	suffix, err := db.ReadSuffix()
	require.NoError(t, err)
	return suffix
}

// --- record fixtures ---

// valueSize is the width every fixture value is padded to, so a test that
// truncates bytes off a file can reason about how many records it damaged.
const valueSize = 64

// BlockHash returns the hash the fixtures file the block at n under. A hash must
// be unique per block, so it is derived from the number.
func BlockHash(n blocktypes.BlockNumber) blocktypes.BlockHash {
	return blocktypes.BlockHash(fmt.Sprintf("block-hash-%020d", n))
}

// RecordValue returns the bytes the fixtures store for kind at first. Values are
// distinct per record, so a read can prove it found the one it asked for.
func RecordValue(label string, first blocktypes.BlockNumber) []byte {
	value := []byte(fmt.Sprintf("%s@%d", label, first))
	if len(value) >= valueSize {
		return value
	}
	return append(value, bytes.Repeat([]byte("."), valueSize-len(value))...)
}

// PutQCGroup stores a QC and block fixtures covering [first, next) atomically.
func PutQCGroup(t *testing.T, db blocktypes.BlockDB, first, next blocktypes.BlockNumber) {
	t.Helper()
	blocks := make([]blocktypes.BlockRecord, 0, next-first)
	for n := first; n < next; n++ {
		blocks = append(blocks, blocktypes.BlockRecord{Number: n, Hash: BlockHash(n), Value: RecordValue("block", n)})
	}
	require.NoError(t, db.PutQC(blocktypes.QCGroupRecord{QC: blocktypes.QCRecord{Data: RecordValue("qc", first)}, Blocks: blocks}))
}

// PutAppData stores an AppProposal and an AppQC, each covering [first, next).
// The pair is what moves the ceiling a prune is capped at.
func PutAppData(t *testing.T, db blocktypes.BlockDB, first, next blocktypes.BlockNumber) {
	t.Helper()
	require.NoError(t, db.PutAppProposal(blocktypes.AppProposalRecord{Data: RecordValue("app_proposal", first), First: first, Next: next}))
	require.NoError(t, db.PutAppQC(blocktypes.AppQCRecord{Data: RecordValue("app_qc", first), First: first, Next: next}))
}

// WriteCohorts stores consecutive atomic QC groups starting at 0, without app data.
func WriteCohorts(t *testing.T, db blocktypes.BlockDB, cohorts, perCohort uint64) {
	t.Helper()
	for i := range cohorts {
		first := blocktypes.BlockNumber(i * perCohort)
		PutQCGroup(t, db, first, first+blocktypes.BlockNumber(perCohort))
	}
}

// WriteAppData stores app data covering each of the cohorts WriteCohorts wrote,
// which is what lifts the ceiling a prune is capped at. It trails the whole run,
// as it does in production, where the application commits behind consensus.
func WriteAppData(t *testing.T, db blocktypes.BlockDB, cohorts, perCohort uint64) {
	t.Helper()
	for i := range cohorts {
		first := blocktypes.BlockNumber(i * perCohort)
		PutAppData(t, db, first, first+blocktypes.BlockNumber(perCohort))
	}
}
