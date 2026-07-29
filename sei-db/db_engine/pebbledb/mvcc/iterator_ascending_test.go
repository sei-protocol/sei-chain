package mvcc

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

const ascIterTestStore = "store1"

// newAscendingIterTestDB seeds a directory the way the legacy ascending-version
// build would have left it -- ascending-encoded data plus a latest-version
// marker, but no descending sentinel -- so OpenDB selects the ascending path.
func newAscendingIterTestDB(t *testing.T) *Database {
	t.Helper()

	dir := t.TempDir()
	raw, err := pebble.Open(dir, &pebble.Options{Comparer: MVCCComparer})
	require.NoError(t, err)
	// Seeded under a different store so it never shows up in the iterations below.
	require.NoError(t, raw.Set(
		MVCCEncodeAscending(prependStoreKey("seedstore", []byte("seed")), 1),
		MVCCEncodeAscending([]byte("seed"), 0),
		pebble.Sync,
	))
	var ts [VersionSize]byte
	ts[0] = 1
	require.NoError(t, raw.Set([]byte(latestVersionKey), ts[:], pebble.Sync))
	require.NoError(t, raw.Close())

	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"
	store, err := OpenDB(dir, cfg)
	require.NoError(t, err)
	db := store.(*Database)
	t.Cleanup(func() { _ = db.Close() })
	require.False(t, db.descending, "seeded legacy DB must open in ascending mode")
	return db
}

// A logical key that is visible at the target version but was also written at a
// later version must still be returned by a reverse iteration at that version.
// The previous implementation seeked past every version of such a key as soon as
// the version it landed on was newer than the target, dropping the key entirely.
func TestAscendingReverseIteratorDoesNotSkipShadowedKey(t *testing.T) {
	db := newAscendingIterTestDB(t)

	applyVersion(t, db, ascIterTestStore, 5, []byte("keyA"), []byte("A@5"))
	applyVersion(t, db, ascIterTestStore, 30, []byte("keyB"), []byte("B@30"))
	applyVersion(t, db, ascIterTestStore, 100, []byte("keyA"), []byte("A@100"))
	applyVersion(t, db, ascIterTestStore, 200, []byte("keyB"), []byte("B@200"))

	itr, err := db.ReverseIterator(ascIterTestStore, 50, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var got []string
	for ; itr.Valid(); itr.Next() {
		got = append(got, fmt.Sprintf("%s=%s", itr.Key(), itr.Value()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"keyB=B@30", "keyA=A@5"}, got)
}

// The same shadowing case in the forward direction.
func TestAscendingForwardIteratorDoesNotSkipShadowedKey(t *testing.T) {
	db := newAscendingIterTestDB(t)

	applyVersion(t, db, ascIterTestStore, 5, []byte("keyA"), []byte("A@5"))
	applyVersion(t, db, ascIterTestStore, 30, []byte("keyB"), []byte("B@30"))
	applyVersion(t, db, ascIterTestStore, 100, []byte("keyA"), []byte("A@100"))
	applyVersion(t, db, ascIterTestStore, 200, []byte("keyB"), []byte("B@200"))

	itr, err := db.Iterator(ascIterTestStore, 50, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var got []string
	for ; itr.Valid(); itr.Next() {
		got = append(got, fmt.Sprintf("%s=%s", itr.Key(), itr.Value()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"keyA=A@5", "keyB=B@30"}, got)
}

// Reverse iteration at an old version must not consume stack proportional to the
// number of keys it has to skip. The child process runs with a small max stack so
// that a per-skipped-key stack frame fails fast instead of needing millions of
// keys to exhaust the default limit.
func TestAscendingReverseIteratorDeepSkipDoesNotOverflowStack(t *testing.T) {
	if os.Getenv("MVCC_ASC_DEEP_SKIP_CHILD") == "1" {
		debug.SetMaxStack(16 << 20)
		runAscendingDeepReverseSkip(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAscendingReverseIteratorDeepSkipDoesNotOverflowStack", "-test.v")
	cmd.Env = append(os.Environ(), "MVCC_ASC_DEEP_SKIP_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\nstack overflow present: %v\n%s",
			err, strings.Contains(string(out), "stack overflow"), lastLinesOfOutput(string(out), 40))
	}
}

func runAscendingDeepReverseSkip(t *testing.T) {
	db := newAscendingIterTestDB(t)

	// One key visible at version 10, then many keys that sort after it and only
	// exist at much later versions, all of which a reverse scan at 10 must skip.
	applyVersion(t, db, ascIterTestStore, 10, []byte("aaa"), []byte("visible"))

	const newKeys = 150_000
	const batch = 1000
	for i := 0; i < newKeys; i += batch {
		pairs := make([]*proto.KVPair, 0, batch)
		for j := 0; j < batch; j++ {
			pairs = append(pairs, &proto.KVPair{
				Key:   []byte(fmt.Sprintf("zzz%08d", i+j)),
				Value: []byte("new"),
			})
		}
		require.NoError(t, db.ApplyChangesetSync(int64(1000+i/batch), []*proto.NamedChangeSet{{
			Name:      ascIterTestStore,
			Changeset: proto.ChangeSet{Pairs: pairs},
		}}))
	}

	itr, err := db.ReverseIterator(ascIterTestStore, 10, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var got []string
	for ; itr.Valid(); itr.Next() {
		got = append(got, fmt.Sprintf("%s=%s", itr.Key(), itr.Value()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"aaa=visible"}, got)
}

// The ascending iterator must agree with the descending iterator, which is the
// primary path for all new DBs. Identical changesets are applied to one DB of
// each encoding and every iteration is compared, covering multi-version keys,
// tombstones, resurrected keys and bounded ranges at several query versions.
func TestAscendingIteratorMatchesDescending(t *testing.T) {
	asc := newAscendingIterTestDB(t)
	desc := newTestDB(t, false)
	require.True(t, desc.descending, "fresh DB must open in descending mode")

	rng := rand.New(rand.NewSource(20260728))
	keys := make([][]byte, 0, 40)
	for i := 0; i < 40; i++ {
		keys = append(keys, []byte(fmt.Sprintf("key%03d", i)))
	}

	const versions = 60
	for v := int64(1); v <= versions; v++ {
		pairs := make([]*proto.KVPair, 0, 8)
		for n := 0; n < 1+rng.Intn(6); n++ {
			k := keys[rng.Intn(len(keys))]
			// Roughly a third of writes are deletions, so tombstones interleave
			// with live versions of the same logical key.
			if rng.Intn(3) == 0 {
				pairs = append(pairs, &proto.KVPair{Key: k, Delete: true})
				continue
			}
			pairs = append(pairs, &proto.KVPair{
				Key:   k,
				Value: []byte(fmt.Sprintf("v%d@%d", rng.Intn(1000), v)),
			})
		}
		cs := []*proto.NamedChangeSet{{
			Name:      ascIterTestStore,
			Changeset: proto.ChangeSet{Pairs: pairs},
		}}
		require.NoError(t, asc.ApplyChangesetSync(v, cs))
		require.NoError(t, desc.ApplyChangesetSync(v, cs))
	}

	ranges := []struct {
		name       string
		start, end []byte
	}{
		{"full", nil, nil},
		{"bounded", []byte("key005"), []byte("key030")},
		{"start-only", []byte("key020"), nil},
		{"end-only", nil, []byte("key010")},
		{"empty", []byte("key900"), nil},
	}

	for _, queryVersion := range []int64{1, 7, 23, 42, versions, versions + 5} {
		for _, r := range ranges {
			for _, reverse := range []bool{false, true} {
				name := fmt.Sprintf("v%d/%s/reverse=%v", queryVersion, r.name, reverse)
				gotAsc := collectIteration(t, asc, queryVersion, r.start, r.end, reverse)
				gotDesc := collectIteration(t, desc, queryVersion, r.start, r.end, reverse)
				require.Equal(t, gotDesc, gotAsc, "ascending/descending mismatch at %s", name)
			}
		}
	}
}

func collectIteration(t *testing.T, db *Database, version int64, start, end []byte, reverse bool) []string {
	t.Helper()

	var (
		itr dbm.Iterator
		err error
	)
	if reverse {
		itr, err = db.ReverseIterator(ascIterTestStore, version, start, end)
	} else {
		itr, err = db.Iterator(ascIterTestStore, version, start, end)
	}
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	got := []string{}
	for ; itr.Valid(); itr.Next() {
		got = append(got, fmt.Sprintf("%s=%s", itr.Key(), itr.Value()))
	}
	require.NoError(t, itr.Error())
	return got
}

func lastLinesOfOutput(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
