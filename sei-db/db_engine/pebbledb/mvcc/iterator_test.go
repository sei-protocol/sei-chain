package mvcc

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sstest "github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

const iterTestStoreKey = "store1"

func newIterTestDB(t *testing.T) types.StateStore {
	t.Helper()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"
	db, err := OpenDB(t.TempDir(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// A logical key that is visible at the target version but was also written at a
// later version must still be returned by a reverse iteration at that version.
func TestReverseIteratorDoesNotSkipShadowedKey(t *testing.T) {
	db := newIterTestDB(t)

	require.NoError(t, sstest.DBApplyChangeset(db, 5, iterTestStoreKey, [][]byte{[]byte("keyA")}, [][]byte{[]byte("A@5")}))
	require.NoError(t, sstest.DBApplyChangeset(db, 30, iterTestStoreKey, [][]byte{[]byte("keyB")}, [][]byte{[]byte("B@30")}))
	require.NoError(t, sstest.DBApplyChangeset(db, 100, iterTestStoreKey, [][]byte{[]byte("keyA")}, [][]byte{[]byte("A@100")}))
	require.NoError(t, sstest.DBApplyChangeset(db, 200, iterTestStoreKey, [][]byte{[]byte("keyB")}, [][]byte{[]byte("B@200")}))

	itr, err := db.ReverseIterator(iterTestStoreKey, 50, nil, nil)
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
// number of keys it has to skip. The child process runs with a small max stack so
// that a per-skipped-key stack frame fails fast instead of needing ~3M keys.
func TestReverseIteratorDeepSkipDoesNotOverflowStack(t *testing.T) {
	if os.Getenv("MVCC_DEEP_SKIP_CHILD") == "1" {
		debug.SetMaxStack(16 << 20)
		runDeepReverseSkip(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestReverseIteratorDeepSkipDoesNotOverflowStack", "-test.v")
	cmd.Env = append(os.Environ(), "MVCC_DEEP_SKIP_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\nstack overflow present: %v\n%s",
			err, strings.Contains(string(out), "stack overflow"), lastLines(string(out), 40))
	}
}

func runDeepReverseSkip(t *testing.T) {
	db := newIterTestDB(t)

	// One key visible at version 10, then many keys that sort after it and only
	// exist at a much later version.
	require.NoError(t, sstest.DBApplyChangeset(db, 10, iterTestStoreKey, [][]byte{[]byte("aaa")}, [][]byte{[]byte("visible")}))

	const newKeys = 150_000
	const batch = 1000
	keys := make([][]byte, 0, batch)
	vals := make([][]byte, 0, batch)
	for i := 0; i < newKeys; i += batch {
		keys = keys[:0]
		vals = vals[:0]
		for j := 0; j < batch; j++ {
			keys = append(keys, []byte(fmt.Sprintf("zzz%08d", i+j)))
			vals = append(vals, []byte("new"))
		}
		require.NoError(t, sstest.DBApplyChangeset(db, int64(1000+i/batch), iterTestStoreKey, keys, vals))
	}

	itr, err := db.ReverseIterator(iterTestStoreKey, 10, nil, nil)
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
