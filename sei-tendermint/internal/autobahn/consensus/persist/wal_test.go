package persist

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func acceptAny(_ string) error { return nil }

// stringCodec is a trivial codec for testing indexedWAL with strings.
type stringCodec struct{}

func (stringCodec) Marshal(s string) []byte            { return []byte(s) }
func (stringCodec) Unmarshal(b []byte) (string, error) { return string(b), nil }

func TestIndexedWAL_EmptyStart(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	require.Equal(t, uint64(0), iw.Count())
	require.Equal(t, iw.FirstIdx(), iw.nextIdx) // empty: firstIdx == nextIdx

	entries, err := iw.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 0, len(entries))

	require.NoError(t, iw.Close())
}

func TestIndexedWAL_WriteAndReadAll(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.NoError(t, iw.Write("c"))

	require.Equal(t, uint64(3), iw.Count())
	require.Equal(t, uint64(1), iw.FirstIdx())
	require.Equal(t, uint64(4), iw.nextIdx)

	entries, err := iw.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 3, len(entries))
	require.Equal(t, "a", entries[0])
	require.Equal(t, "b", entries[1])
	require.Equal(t, "c", entries[2])

	require.NoError(t, iw.Close())
}

func TestIndexedWAL_ReopenWithData(t *testing.T) {
	dir := t.TempDir()

	// Write some entries and close.
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.NoError(t, iw.Write("x"))
	require.NoError(t, iw.Write("y"))
	require.NoError(t, iw.Close())

	// Reopen — should recover firstIdx, nextIdx, and entries.
	iw2, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	require.Equal(t, uint64(2), iw2.Count())
	require.Equal(t, uint64(1), iw2.FirstIdx())
	require.Equal(t, uint64(3), iw2.nextIdx)

	entries, err := iw2.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 2, len(entries))
	require.Equal(t, "x", entries[0])
	require.Equal(t, "y", entries[1])

	require.NoError(t, iw2.Close())
}

func TestIndexedWAL_ReopenAfterTruncate(t *testing.T) {
	dir := t.TempDir()

	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, iw.Write(s))
	}
	// Truncate first 3 entries (indices 1,2,3); keep 4,5.
	require.NoError(t, iw.TruncateBefore(4, acceptAny))
	require.Equal(t, uint64(2), iw.Count())
	require.NoError(t, iw.Close())

	// Reopen — should see only the surviving entries.
	iw2, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), iw2.Count())
	require.Equal(t, uint64(4), iw2.FirstIdx())
	require.Equal(t, uint64(6), iw2.nextIdx)

	entries, err := iw2.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 2, len(entries))
	require.Equal(t, "d", entries[0])
	require.Equal(t, "e", entries[1])

	require.NoError(t, iw2.Close())
}

func TestIndexedWAL_TruncateAllButLast(t *testing.T) {
	dir := t.TempDir()

	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.NoError(t, iw.Write("c"))

	// TruncateBefore keeps the entry at the given index; remove all but last.
	require.NoError(t, iw.TruncateBefore(3, acceptAny))
	require.Equal(t, uint64(1), iw.Count())
	require.Equal(t, uint64(3), iw.FirstIdx())
	require.NoError(t, iw.Close())

	// Reopen — should see one entry.
	iw2, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), iw2.Count())

	entries, err := iw2.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "c", entries[0])

	require.NoError(t, iw2.Close())
}

func TestIndexedWAL_TruncateBeforeVerifiesEntry(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.NoError(t, iw.Write("c"))

	// Verify callback receives the correct entry.
	var got string
	require.NoError(t, iw.TruncateBefore(2, func(s string) error {
		got = s
		return nil
	}))
	require.Equal(t, "b", got)
	require.Equal(t, uint64(2), iw.FirstIdx())

	// Verify callback can reject the truncation.
	err = iw.TruncateBefore(3, func(s string) error {
		return fmt.Errorf("rejected: %s", s)
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rejected: c")
	// firstIdx should NOT have advanced since verify rejected.
	require.Equal(t, uint64(2), iw.FirstIdx())

	require.NoError(t, iw.Close())
}

func TestIndexedWAL_TruncateAll(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.NoError(t, iw.Write("c"))
	require.Equal(t, uint64(3), iw.Count())
	require.Equal(t, uint64(4), iw.nextIdx)

	require.NoError(t, iw.TruncateAll())
	require.Equal(t, uint64(0), iw.Count())
	require.Equal(t, uint64(4), iw.FirstIdx()) // advanced to nextIdx
	require.Equal(t, uint64(4), iw.nextIdx)    // index counter preserved

	// Can write fresh entries after TruncateAll; indices continue.
	require.NoError(t, iw.Write("x"))
	require.Equal(t, uint64(1), iw.Count())
	require.Equal(t, uint64(4), iw.FirstIdx())
	require.Equal(t, uint64(5), iw.nextIdx)

	entries, err := iw.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "x", entries[0])

	require.NoError(t, iw.Close())

	// Reopen — should see only the post-TruncateAll entry.
	iw2, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), iw2.Count())
	require.Equal(t, uint64(4), iw2.FirstIdx())
	require.Equal(t, uint64(5), iw2.nextIdx)
	entries, err = iw2.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "x", entries[0])
	require.NoError(t, iw2.Close())
}

func TestIndexedWAL_ReadAllDetectsStaleNextIdx(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.Equal(t, uint64(2), iw.Count())

	// Simulate stale internal state: advance nextIdx so Count() reports more
	// entries than the WAL actually contains. ReadAll must return an error
	// (either from Replay failing to read the missing entry, or from the
	// post-replay count check).
	iw.nextIdx++

	_, err = iw.ReadAll()
	require.Error(t, err)

	iw.nextIdx--
	require.NoError(t, iw.Close())
}

func TestIndexedWAL_WriteAfterTruncate(t *testing.T) {
	dir := t.TempDir()

	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.NoError(t, iw.Write("c"))

	// Truncate "a" and "b".
	require.NoError(t, iw.TruncateBefore(3, acceptAny))
	require.Equal(t, uint64(1), iw.Count())

	// Write more after truncation.
	require.NoError(t, iw.Write("d"))
	require.NoError(t, iw.Write("e"))
	require.Equal(t, uint64(3), iw.Count())
	require.Equal(t, uint64(3), iw.FirstIdx())
	require.Equal(t, uint64(6), iw.nextIdx)

	entries, err := iw.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 3, len(entries))
	require.Equal(t, "c", entries[0])
	require.Equal(t, "d", entries[1])
	require.Equal(t, "e", entries[2])

	require.NoError(t, iw.Close())
}

func TestIndexedWAL_TruncateAllOnEmpty(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	// TruncateAll on a brand-new (empty) WAL should be a no-op.
	require.NoError(t, iw.TruncateAll())
	require.Equal(t, uint64(0), iw.Count())

	// Can still write after TruncateAll on empty.
	require.NoError(t, iw.Write("a"))
	require.Equal(t, uint64(1), iw.Count())

	entries, err := iw.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "a", entries[0])
	require.NoError(t, iw.Close())
}

func TestIndexedWAL_ReopenAfterTruncateAllNoWrites(t *testing.T) {
	dir := t.TempDir()

	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.NoError(t, iw.Write("a"))
	require.NoError(t, iw.Write("b"))
	require.NoError(t, iw.TruncateAll())
	require.Equal(t, uint64(0), iw.Count())
	// Close immediately — no writes after TruncateAll.
	require.NoError(t, iw.Close())

	// Reopen — should be empty with correct index tracking.
	iw2, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)
	require.Equal(t, uint64(0), iw2.Count())

	entries, err := iw2.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 0, len(entries))

	// Writing after reopen should work.
	require.NoError(t, iw2.Write("c"))
	require.Equal(t, uint64(1), iw2.Count())

	entries, err = iw2.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 1, len(entries))
	require.Equal(t, "c", entries[0])
	require.NoError(t, iw2.Close())
}

func TestIndexedWAL_SuccessiveTruncateBefore(t *testing.T) {
	dir := t.TempDir()
	iw, err := openIndexedWAL(dir, stringCodec{})
	require.NoError(t, err)

	for _, s := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, iw.Write(s))
	}
	// First truncate: remove "a" (index 1).
	require.NoError(t, iw.TruncateBefore(2, acceptAny))
	require.Equal(t, uint64(4), iw.Count())
	require.Equal(t, uint64(2), iw.FirstIdx())

	// Write more.
	require.NoError(t, iw.Write("f"))
	require.Equal(t, uint64(5), iw.Count())

	// Second truncate: remove "b", "c", "d" (indices 2,3,4).
	require.NoError(t, iw.TruncateBefore(5, acceptAny))
	require.Equal(t, uint64(2), iw.Count())
	require.Equal(t, uint64(5), iw.FirstIdx())

	entries, err := iw.ReadAll()
	require.NoError(t, err)
	require.Equal(t, 2, len(entries))
	require.Equal(t, "e", entries[0])
	require.Equal(t, "f", entries[1])
	require.NoError(t, iw.Close())
}
