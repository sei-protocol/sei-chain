package mvcc

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

const ascTestStore = "store1"

// newAscendingTestDB seeds a directory the way the legacy ascending-version
// build would have left it -- ascending-encoded data plus a latest-version
// marker, but no descending sentinel -- so OpenDB selects the ascending path.
func newAscendingTestDB(t *testing.T) *Database {
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
	require.False(t, db.descending, "test DB must operate in ascending mode")
	return db
}

// A logical key that is visible at the target version but was also written at a
// later version must still be returned by a reverse iteration at that version.
// The previous implementation seeked past the whole key whenever the version it
// landed on was newer than the target.
func TestAscendingReverseIteratorDoesNotSkipShadowedKey(t *testing.T) {
	db := newAscendingTestDB(t)

	applyVersion(t, db, ascTestStore, 5, []byte("keyA"), []byte("A@5"))
	applyVersion(t, db, ascTestStore, 30, []byte("keyB"), []byte("B@30"))
	applyVersion(t, db, ascTestStore, 100, []byte("keyA"), []byte("A@100"))
	applyVersion(t, db, ascTestStore, 200, []byte("keyB"), []byte("B@200"))

	itr, err := db.ReverseIterator(ascTestStore, 50, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var got []string
	for ; itr.Valid(); itr.Next() {
		got = append(got, fmt.Sprintf("%s=%s", itr.Key(), itr.Value()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"keyB=B@30", "keyA=A@5"}, got)
}

// Reverse iteration at an old version must not consume stack proportional to the
// number of keys it has to skip. The child process runs with a small max stack
// so that a per-skipped-key stack frame fails fast instead of needing millions
// of keys to exhaust the default limit.
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
			err, strings.Contains(string(out), "stack overflow"), lastLines(string(out), 40))
	}
}

func runAscendingDeepReverseSkip(t *testing.T) {
	db := newAscendingTestDB(t)

	// One key visible at version 10, then many keys that sort after it and only
	// exist at much later versions, all of which a reverse scan at 10 must skip.
	applyVersion(t, db, ascTestStore, 10, []byte("aaa"), []byte("visible"))

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
			Name:      ascTestStore,
			Changeset: proto.ChangeSet{Pairs: pairs},
		}}))
	}

	itr, err := db.ReverseIterator(ascTestStore, 10, nil, nil)
	require.NoError(t, err)
	defer func() { _ = itr.Close() }()

	var got []string
	for ; itr.Valid(); itr.Next() {
		got = append(got, fmt.Sprintf("%s=%s", itr.Key(), itr.Value()))
	}
	require.NoError(t, itr.Error())
	require.Equal(t, []string{"aaa=visible"}, got)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
