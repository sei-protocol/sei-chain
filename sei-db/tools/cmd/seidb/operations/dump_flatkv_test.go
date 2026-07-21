package operations

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/stretchr/testify/require"
)

// TestDumpFlatKVFromStoreAllBuckets seeds a mix of account, code, storage and
// misc rows, runs dumpFlatKVFromStore across all four buckets, and checks
// that every file gets the right header, the right number of data lines, and
// the right format. Physical keys are emitted verbatim (no logical
// stripping), which is the contract dump-flatkv promises.
func TestDumpFlatKVFromStoreAllBuckets(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	addrB := addrN(0x22)

	evmCS := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrA, 1),
			noncePair(addrB, 2),
			codePair(addrA, []byte{0x60, 0x80}),
			storagePair(addrA, slotN(0x01), 0xAA),
			storagePair(addrA, slotN(0x02), 0xBB),
			storagePair(addrB, slotN(0x01), 0xCC),
		}},
	}
	bankCS := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("supply/usei"), Value: []byte("100")},
		}},
	}

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{evmCS, bankCS}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, dumpFlatKVFromStore(store, outDir, store.Version(), "", true, false, true, 0))

	type expect struct {
		lines int
	}
	want := map[string]expect{
		"account": {lines: 2}, // 2 nonces -> 2 account rows
		"code":    {lines: 1}, // 1 code
		"storage": {lines: 3}, // 3 storage slots
		"misc":    {lines: 1}, // 1 bank row
	}

	for name, w := range want {
		path := filepath.Join(outDir, name)
		f, err := os.Open(path)
		require.NoError(t, err, "bucket file %s must exist", name)
		defer f.Close() //nolint:errcheck // test cleanup

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

		require.True(t, scanner.Scan(), "missing header in %s", name)
		header := scanner.Text()
		require.True(t,
			strings.HasPrefix(header, "Bucket "+name+" at version "),
			"unexpected header in %s: %q", name, header,
		)

		data := 0
		for scanner.Scan() {
			line := scanner.Text()
			require.True(t, strings.HasPrefix(line, "Key: "),
				"bucket %s: expected line to start with 'Key: ', got %q", name, line)
			require.Contains(t, line, ", Value: ",
				"bucket %s: expected ', Value: ' separator, got %q", name, line)
			data++
		}
		require.NoError(t, scanner.Err(), "scanner error on %s", name)
		require.Equal(t, w.lines, data,
			"bucket %s: expected %d data lines, got %d", name, w.lines, data)
	}
}

// TestDumpFlatKVFromStoreSingleBucket verifies the --bucket filter keeps
// writes restricted to exactly one file even though the iterator still
// walks every DB under the hood.
func TestDumpFlatKVFromStoreSingleBucket(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	evmCS := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrA, 1),
			codePair(addrA, []byte{0x60}),
			storagePair(addrA, slotN(0x01), 0xAA),
		}},
	}
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{evmCS}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)

	outDir := t.TempDir()
	require.NoError(t, dumpFlatKVFromStore(store, outDir, store.Version(), "storage", true, false, true, 0))

	// Only storage file should exist; the others must not be created.
	for _, name := range flatkvBucketOrder {
		path := filepath.Join(outDir, name)
		_, statErr := os.Stat(path)
		if name == "storage" {
			require.NoError(t, statErr, "storage bucket file must exist")
		} else {
			require.True(t, os.IsNotExist(statErr),
				"bucket %s: expected file to be absent when --bucket=storage, got err=%v",
				name, statErr)
		}
	}
}

// TestBucketLtHasherMatchesSingleShot proves the streaming, batched
// bucketLtHasher produces the same checksum as a single ComputeLtHash over the
// same pairs (associativity of the LtHash group), and that the MixIn of the
// per-bucket accumulators equals a single LtHash over the union — the exact
// per-module + total relationship the dump command reports.
func TestBucketLtHasherMatchesSingleShot(t *testing.T) {
	// More than one batch so the incremental MixIn path is exercised.
	n := lthashBatchCap*2 + 17
	all := make([]lthash.KVPairWithLastValue, 0, n)
	hashers := map[string]*bucketLtHasher{
		flatkvBucketAccount: newBucketLtHasher(),
		flatkvBucketStorage: newBucketLtHasher(),
	}
	bucketPairs := map[string][]lthash.KVPairWithLastValue{}

	for i := 0; i < n; i++ {
		bucket := flatkvBucketAccount
		if i%2 == 0 {
			bucket = flatkvBucketStorage
		}
		key := []byte{byte(bucket[0]), byte(i), byte(i >> 8), byte(i >> 16)}
		val := []byte{byte(i), 0xAB, byte(i >> 8)}
		hashers[bucket].add(key, val)
		bucketPairs[bucket] = append(bucketPairs[bucket], lthash.KVPairWithLastValue{Key: key, Value: val})
		all = append(all, lthash.KVPairWithLastValue{Key: key, Value: val})
	}

	total := lthash.New()
	for bucket, h := range hashers {
		h.flush()
		single, _ := lthash.ComputeLtHash(nil, bucketPairs[bucket])
		require.Equal(t, single.Checksum(), h.acc.Checksum(),
			"batched bucket hash for %s must equal single-shot ComputeLtHash", bucket)
		total.MixIn(h.acc)
	}

	unionSingle, _ := lthash.ComputeLtHash(nil, all)
	require.Equal(t, unionSingle.Checksum(), total.Checksum(),
		"MixIn of per-bucket hashes must equal the LtHash over the union of all pairs")
}

// TestSnapshotMetadataMakesCommittedHashFullState pins the decision that
// drives whether dump-flatkv --lthash verifies or skips: a snapshot at version
// 0 is always full-state; a snapshot at version > 0 is full-state iff it
// carried the LtHash metadata key. Presence matters, not the hash value,
// because a legitimate LtHash watermark can be all-zero.
func TestSnapshotMetadataMakesCommittedHashFullState(t *testing.T) {
	require.True(t, snapshotMetadataMakesCommittedHashFullState(0, false),
		"version 0 baseline is always full-state")
	require.True(t, snapshotMetadataMakesCommittedHashFullState(0, true),
		"version 0 baseline is full-state regardless of metadata presence")
	require.False(t, snapshotMetadataMakesCommittedHashFullState(100, false),
		"version>0 without the LtHash metadata key predates LtHash metadata: not full-state")
	require.True(t, snapshotMetadataMakesCommittedHashFullState(100, true),
		"version>0 with the LtHash metadata key present is full-state")
}

func TestSelectedSnapshotHasLtHashMetadata(t *testing.T) {
	dbDir := t.TempDir()
	snapshotName := flatkvSnapshotPrefix + "00000000000000000100"

	hasMetadata, err := selectedSnapshotHasLtHashMetadata(dbDir, snapshotName)
	require.NoError(t, err)
	require.False(t, hasMetadata, "missing metadata dir means the snapshot has no LtHash metadata")

	metaDir := filepath.Join(dbDir, snapshotName, flatkvMetadataDir)
	require.NoError(t, os.MkdirAll(metaDir, 0o750))
	cfg := pebbledb.DefaultConfig()
	cfg.DataDir = metaDir
	cfg.EnableMetrics = false
	db, err := pebbledb.Open(context.Background(), &cfg)
	require.NoError(t, err)

	hasMetadata, err = selectedSnapshotHasLtHashMetadata(dbDir, snapshotName)
	require.NoError(t, err)
	require.False(t, hasMetadata, "metadata dir without MetaLtHashKey is still pre-LtHash")

	zeroHash := lthash.New()
	require.NoError(t, db.Set(ktype.MetaLtHashKey, zeroHash.Marshal(), dbtypes.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	hasMetadata, err = selectedSnapshotHasLtHashMetadata(dbDir, snapshotName)
	require.NoError(t, err)
	require.True(t, hasMetadata, "MetaLtHashKey presence matters even when the stored watermark is all-zero")
}

// TestDumpFlatKVFromStoreSkipsVerifyWhenNotFullState confirms that passing
// committedIsFullState=false skips LtHash verification (returns nil) rather
// than comparing a full re-scan against a partial committed hash.
func TestDumpFlatKVFromStoreSkipsVerifyWhenNotFullState(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			storagePair(addrA, slotN(0x01), 0xAA),
		}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)

	outDir := t.TempDir()
	// committedIsFullState=false -> verification is skipped, so the dump
	// succeeds even though we are not cross-checking the committed hash.
	require.NoError(t, dumpFlatKVFromStore(store, outDir, store.Version(), "", true, false, false, 0))
}

func TestDumpFlatKVFromStoreLtHashOnlyWritesNoBucketFiles(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			noncePair(addrA, 1),
			codePair(addrA, []byte{0x60}),
			storagePair(addrA, slotN(0x01), 0xAA),
		}},
	}}))
	_, err := store.Commit(store.Version() + 1)
	require.NoError(t, err)

	outDir := filepath.Join(t.TempDir(), "must-not-be-created")
	require.NoError(t, dumpFlatKVFromStore(store, outDir, store.Version(), "", true, true, true, 0))
	_, statErr := os.Stat(outDir)
	require.True(t, os.IsNotExist(statErr), "lthash-only mode must not create output dir or bucket files")
}

func TestIsFlatKVBucket(t *testing.T) {
	for _, b := range flatkvBucketOrder {
		require.True(t, isFlatKVBucket(b), "%s should be accepted", b)
	}
	require.False(t, isFlatKVBucket(""), "empty should not validate")
	require.False(t, isFlatKVBucket("metadata"), "metadata is intentionally excluded from dump-flatkv")
	require.False(t, isFlatKVBucket("evm"), "evm is a module, not a bucket")
}
