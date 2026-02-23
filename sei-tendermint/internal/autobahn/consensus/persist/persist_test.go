package persist

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// testMsg creates a PersistedWrapper with the given data payload, used as
// a convenient proto.Message for testing the generic Persister.
func testMsg(data string) *pb.PersistedWrapper {
	return &pb.PersistedWrapper{Data: []byte(data)}
}

func testMsgData(msg *pb.PersistedWrapper) string {
	return string(msg.GetData())
}

func TestPersisterAlternates(t *testing.T) {
	dir := t.TempDir()

	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)

	// Both files should be pre-created (empty) by NewPersister for dir-sync optimization.
	_, errA := os.Stat(filepath.Join(dir, "test"+suffixA))
	_, errB := os.Stat(filepath.Join(dir, "test"+suffixB))
	require.NoError(t, errA, "A should be pre-created")
	require.NoError(t, errB, "B should be pre-created")

	// First write: goes to A (seq=1)
	require.NoError(t, w.Persist(testMsg("data1")))

	// Second write: goes to B (seq=2)
	require.NoError(t, w.Persist(testMsg("data2")))

	// Third write: goes to A (seq=3, overwrite)
	require.NoError(t, w.Persist(testMsg("data3")))

	// loadPersisted should return data3 (highest seq)
	wrapper, err := loadPersisted(dir, "test")
	require.NoError(t, err)
	var inner pb.PersistedWrapper
	require.NoError(t, proto.Unmarshal(wrapper.GetData(), &inner))
	require.Equal(t, "data3", testMsgData(&inner))
}

func TestPersisterPicksHigherSeq(t *testing.T) {
	dir := t.TempDir()

	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)

	// Write three times: A(seq=1), B(seq=2), A(seq=3)
	require.NoError(t, w.Persist(testMsg("first")))
	require.NoError(t, w.Persist(testMsg("second")))
	require.NoError(t, w.Persist(testMsg("third")))

	// Should load "third" (seq=3 > seq=2)
	w2, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "third", testMsgData(msg))
	_ = w2
}

func TestLoadPersistedOneCorruptFileSucceeds(t *testing.T) {
	dir := t.TempDir()

	// Write to both files: A(seq=1), B(seq=2)
	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(testMsg("first")))  // seq=1, A
	require.NoError(t, w.Persist(testMsg("second"))) // seq=2, B

	// Corrupt B (the winner) — should fall back to A
	err = os.WriteFile(filepath.Join(dir, "test"+suffixB), []byte("corrupt"), 0600)
	require.NoError(t, err)

	_, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "first", testMsgData(msg))
}

func TestLoadPersistedBothCorruptError(t *testing.T) {
	dir := t.TempDir()

	// Write valid data to A only
	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(testMsg("valid"))) // seq=1, A

	// Corrupt A (B is empty, treated as non-existent) — both files fail
	err = os.WriteFile(filepath.Join(dir, "test"+suffixA), []byte("corrupt"), 0600)
	require.NoError(t, err)

	_, _, err = NewPersister[*pb.PersistedWrapper](dir, "test")
	require.Error(t, err)
}

func TestNewPersisterOneCorruptFileSucceeds(t *testing.T) {
	dir := t.TempDir()

	// Write to both files: A(seq=1), B(seq=2)
	w1, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w1.Persist(testMsg("first")))  // seq=1, A
	require.NoError(t, w1.Persist(testMsg("second"))) // seq=2, B

	// Corrupt B (the winner) — NewPersister should still succeed using A
	err = os.WriteFile(filepath.Join(dir, "test"+suffixB), []byte("corrupt"), 0600)
	require.NoError(t, err)

	w2, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "first", testMsgData(msg))

	// A won (seq=1), so next write goes to B (the corrupt/loser slot)
	require.NoError(t, w2.Persist(testMsg("recovered"))) // seq=2, B

	_, loaded2, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg2, ok := loaded2.Get()
	require.True(t, ok)
	require.Equal(t, "recovered", testMsgData(msg2))
}

func TestLoadPersistedEmptyDir(t *testing.T) {
	dir := t.TempDir()

	// No files exist
	_, err := loadPersisted(dir, "test")
	require.True(t, errors.Is(err, ErrNoData), "should return ErrNoData when no files exist")
}

func TestNewPersisterInvalidDirError(t *testing.T) {
	// State dir must already exist; invalid (nonexistent or not a directory) returns error
	_, _, err := NewPersister[*pb.PersistedWrapper]("/nonexistent/path/that/does/not/exist", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid state dir")
}

func TestPersistWriteErrorReturnsError(t *testing.T) {
	// When the directory becomes inaccessible (e.g. permission denied),
	// Persist returns error from OpenFile.
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 on directory not reliable on Windows")
	}
	dir := t.TempDir()
	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(testMsg("data1")))
	// Remove all permissions from dir so OpenFile fails with EACCES
	require.NoError(t, os.Chmod(dir, 0000))
	defer os.Chmod(dir, 0700) //nolint:errcheck
	err = w.Persist(testMsg("data2"))
	require.Error(t, err)
}

func TestPersistWriteErrorDoesNotAdvanceSeq(t *testing.T) {
	// When Persist fails (e.g. permission denied), seq must not advance.
	// Otherwise the next successful write would target the same file as the
	// last good write, destroying the only valid backup.
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 on directory not reliable on Windows")
	}
	dir := t.TempDir()
	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)

	// Successful write: seq=1 → A
	require.NoError(t, w.Persist(testMsg("good")))

	// Make dir unwritable so next Persist fails
	require.NoError(t, os.Chmod(dir, 0000))
	err = w.Persist(testMsg("fail"))
	require.Error(t, err)
	require.NoError(t, os.Chmod(dir, 0700))

	// Next successful write should go to B (seq=2), preserving A.
	// If seq had incorrectly advanced during the failed write, this would
	// write to A (seq=3), overwriting our only good data.
	require.NoError(t, w.Persist(testMsg("recovered")))

	// Verify: "recovered" is latest.
	_, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "recovered", testMsgData(msg))
}

func TestLoadPersistedOSErrorPropagates(t *testing.T) {
	// When one A/B file has an OS-level error (not corrupt data),
	// loadPersisted should propagate the error instead of silently
	// falling back to the other file.
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 on file not reliable on Windows")
	}
	dir := t.TempDir()

	// Write to both files: A(seq=1), B(seq=2)
	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(testMsg("first")))  // seq=1, A
	require.NoError(t, w.Persist(testMsg("second"))) // seq=2, B

	// Make B unreadable (OS error, not corrupt data)
	pathB := filepath.Join(dir, "test"+suffixB)
	require.NoError(t, os.Chmod(pathB, 0000))
	defer os.Chmod(pathB, 0600) //nolint:errcheck

	// loadPersisted should fail — not silently fall back to A
	_, err = loadPersisted(dir, "test")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrCorrupt), "OS error should not be wrapped as ErrCorrupt")
	require.False(t, errors.Is(err, ErrNoData), "should not be ErrNoData")
}

func TestLoadPersistedCorruptFallsBack(t *testing.T) {
	// When one file is corrupt (unmarshal error), loadPersisted should
	// fall back to the other file — this is the crash recovery path.
	dir := t.TempDir()

	w, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(testMsg("first")))  // seq=1, A
	require.NoError(t, w.Persist(testMsg("second"))) // seq=2, B

	// Corrupt B (simulates crash mid-write)
	err = os.WriteFile(filepath.Join(dir, "test"+suffixB), []byte("garbage"), 0600)
	require.NoError(t, err)

	// Should succeed using A, since B's error is ErrCorrupt (tolerable)
	_, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "first", testMsgData(msg))
}

func TestNewPersisterResumeSeq(t *testing.T) {
	dir := t.TempDir()

	// Create persister and write some data
	w1, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w1.Persist(testMsg("data1"))) // seq=1, A
	require.NoError(t, w1.Persist(testMsg("data2"))) // seq=2, B
	require.NoError(t, w1.Persist(testMsg("data3"))) // seq=3, A (winner)

	// Create new persister (simulates restart)
	// A has seq=3 (winner), so new persister should write to B first to preserve A
	w2, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w2.Persist(testMsg("data4"))) // seq=4, B (preserves A)

	// Verify data4 is the latest (seq=4)
	_, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "data4", testMsgData(msg))
}

func TestNewPersisterPreservesWinner(t *testing.T) {
	dir := t.TempDir()

	// Write to both files: A=seq1, B=seq2 (B wins)
	w1, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w1.Persist(testMsg("old")))    // seq=1, A
	require.NoError(t, w1.Persist(testMsg("winner"))) // seq=2, B (winner)

	// New persister should write to A first (preserve B)
	w2, _, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	require.NoError(t, w2.Persist(testMsg("new"))) // seq=3, A (preserves B)

	// Verify "new" is the latest (seq=3)
	_, loaded, err := NewPersister[*pb.PersistedWrapper](dir, "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "new", testMsgData(msg))
}
