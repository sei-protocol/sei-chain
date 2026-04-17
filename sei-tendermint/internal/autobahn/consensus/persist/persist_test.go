package persist

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPersisterAlternates(t *testing.T) {
	dir := t.TempDir()

	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)

	// Both files should be pre-created (empty) by NewPersister for dir-sync optimization.
	_, errA := os.Stat(filepath.Join(dir, "test"+suffixA))
	_, errB := os.Stat(filepath.Join(dir, "test"+suffixB))
	require.NoError(t, errA, "A should be pre-created")
	require.NoError(t, errB, "B should be pre-created")

	// First write: goes to A (seq=1)
	require.NoError(t, w.Persist(wrapperspb.String("data1")))

	// Second write: goes to B (seq=2)
	require.NoError(t, w.Persist(wrapperspb.String("data2")))

	// Third write: goes to A (seq=3, overwrite)
	require.NoError(t, w.Persist(wrapperspb.String("data3")))

	// loadPersisted should return data3 (highest seq)
	ds, err := loadPersisted(dir, "test")
	require.NoError(t, err)
	var inner wrapperspb.StringValue
	require.NoError(t, proto.Unmarshal(ds.data, &inner))
	require.Equal(t, "data3", inner.GetValue())
}

func TestPersisterPicksHigherSeq(t *testing.T) {
	dir := t.TempDir()

	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)

	// Write three times: A(seq=1), B(seq=2), A(seq=3)
	require.NoError(t, w.Persist(wrapperspb.String("first")))
	require.NoError(t, w.Persist(wrapperspb.String("second")))
	require.NoError(t, w.Persist(wrapperspb.String("third")))

	// Should load "third" (seq=3 > seq=2)
	w2, loaded, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "third", msg.GetValue())
	_ = w2
}

func TestLoadPersistedOneCorruptFileSucceeds(t *testing.T) {
	dir := t.TempDir()

	// Write to both files: A(seq=1), B(seq=2)
	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("first")))  // seq=1, A
	require.NoError(t, w.Persist(wrapperspb.String("second"))) // seq=2, B

	// Corrupt B (the winner) — should fall back to A
	err = os.WriteFile(filepath.Join(dir, "test"+suffixB), []byte("corrupt"), 0600)
	require.NoError(t, err)

	_, loaded, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "first", msg.GetValue())
}

func TestLoadPersistedBothCorruptError(t *testing.T) {
	dir := t.TempDir()

	// Write valid data to A only
	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("valid"))) // seq=1, A

	// Corrupt A (B is empty, treated as non-existent) — both files fail
	err = os.WriteFile(filepath.Join(dir, "test"+suffixA), []byte("corrupt"), 0600)
	require.NoError(t, err)

	_, _, err = NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.Error(t, err)
}

func TestNewPersisterOneCorruptFileSucceeds(t *testing.T) {
	dir := t.TempDir()

	// Write to both files: A(seq=1), B(seq=2)
	w1, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w1.Persist(wrapperspb.String("first")))  // seq=1, A
	require.NoError(t, w1.Persist(wrapperspb.String("second"))) // seq=2, B

	// Corrupt B (the winner) — NewPersister should still succeed using A
	err = os.WriteFile(filepath.Join(dir, "test"+suffixB), []byte("corrupt"), 0600)
	require.NoError(t, err)

	w2, loaded, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "first", msg.GetValue())

	// A won (seq=1), so next write goes to B (the corrupt/loser slot)
	require.NoError(t, w2.Persist(wrapperspb.String("recovered"))) // seq=2, B

	_, loaded2, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg2, ok := loaded2.Get()
	require.True(t, ok)
	require.Equal(t, "recovered", msg2.GetValue())
}

func TestLoadPersistedEmptyDir(t *testing.T) {
	dir := t.TempDir()

	// No files exist
	_, err := loadPersisted(dir, "test")
	require.True(t, errors.Is(err, ErrNoData), "should return ErrNoData when no files exist")
}

func TestNewPersisterInvalidDirError(t *testing.T) {
	// State dir must already exist; invalid (nonexistent or not a directory) returns error
	_, _, err := NewPersister[*wrapperspb.StringValue](utils.Some("/nonexistent/path/that/does/not/exist"), "test")
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
	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("data1")))
	// Remove all permissions from dir so OpenFile fails with EACCES
	require.NoError(t, os.Chmod(dir, 0000))
	defer os.Chmod(dir, 0700) //nolint:errcheck
	err = w.Persist(wrapperspb.String("data2"))
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
	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)

	// Successful write: seq=1 → A
	require.NoError(t, w.Persist(wrapperspb.String("good")))

	// Make dir unwritable so next Persist fails
	require.NoError(t, os.Chmod(dir, 0000))
	err = w.Persist(wrapperspb.String("fail"))
	require.Error(t, err)
	require.NoError(t, os.Chmod(dir, 0700))

	// Next successful write should go to B (seq=2), preserving A.
	// If seq had incorrectly advanced during the failed write, this would
	// write to A (seq=3), overwriting our only good data.
	require.NoError(t, w.Persist(wrapperspb.String("recovered")))

	// Verify: "recovered" is latest.
	_, loaded, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "recovered", msg.GetValue())
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
	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("first")))  // seq=1, A
	require.NoError(t, w.Persist(wrapperspb.String("second"))) // seq=2, B

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

func TestNewPersisterResumeSeq(t *testing.T) {
	dir := t.TempDir()

	// Create persister and write some data
	w1, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w1.Persist(wrapperspb.String("data1"))) // seq=1, A
	require.NoError(t, w1.Persist(wrapperspb.String("data2"))) // seq=2, B
	require.NoError(t, w1.Persist(wrapperspb.String("data3"))) // seq=3, A (winner)

	// Create new persister (simulates restart)
	// A has seq=3 (winner), so new persister should write to B first to preserve A
	w2, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w2.Persist(wrapperspb.String("data4"))) // seq=4, B (preserves A)

	// Verify data4 is the latest (seq=4)
	_, loaded, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "data4", msg.GetValue())
}

func TestNewPersisterPreservesWinner(t *testing.T) {
	dir := t.TempDir()

	// Write to both files: A=seq1, B=seq2 (B wins)
	w1, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w1.Persist(wrapperspb.String("old")))    // seq=1, A
	require.NoError(t, w1.Persist(wrapperspb.String("winner"))) // seq=2, B (winner)

	// New persister should write to A first (preserve B)
	w2, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w2.Persist(wrapperspb.String("new"))) // seq=3, A (preserves B)

	// Verify "new" is the latest (seq=3)
	_, loaded, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	msg, ok := loaded.Get()
	require.True(t, ok)
	require.Equal(t, "new", msg.GetValue())
}

// --- CRC32 and file header validation tests ---

func TestLoadFileDataCorruption(t *testing.T) {
	dir := t.TempDir()

	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("data"))) // seq=1, A

	// Flip a byte in the proto payload; CRC should catch it.
	path := filepath.Join(dir, "test"+suffixA)
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	bz[len(bz)-1] ^= 0xFF
	require.NoError(t, os.WriteFile(path, bz, 0600))

	_, err = loadFile(dir, "test"+suffixA)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCorrupt))
	require.Contains(t, err.Error(), "crc32 mismatch")
}

func TestLoadFileTruncatedHeader(t *testing.T) {
	dir := t.TempDir()

	// A file shorter than the header is rejected.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test"+suffixA), []byte("short"), 0600))

	_, err := loadFile(dir, "test"+suffixA)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCorrupt))
	require.Contains(t, err.Error(), "truncated")
}

func TestLoadFileZeroSeq(t *testing.T) {
	dir := t.TempDir()

	// Build a valid-CRC file with seq=0.
	payload := make([]byte, seqSize)
	binary.LittleEndian.PutUint64(payload, 0)
	crc := crc32.Checksum(payload, crc32c)
	buf := make([]byte, crcSize+len(payload))
	binary.BigEndian.PutUint32(buf[:crcSize], crc)
	copy(buf[crcSize:], payload)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test"+suffixA), buf, 0600))

	_, err := loadFile(dir, "test"+suffixA)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCorrupt))
	require.Contains(t, err.Error(), "zero seq")
}

func TestLoadFileSeqCorruption(t *testing.T) {
	dir := t.TempDir()

	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("data")))

	// Flip a bit in the seq portion of the file; CRC should catch it.
	path := filepath.Join(dir, "test"+suffixA)
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	bz[crcSize] ^= 0x01 // first byte of seq
	require.NoError(t, os.WriteFile(path, bz, 0600))

	_, err = loadFile(dir, "test"+suffixA)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCorrupt))
	require.Contains(t, err.Error(), "crc32 mismatch")
}

func TestPersistFileFormat(t *testing.T) {
	dir := t.TempDir()
	w, _, err := NewPersister[*wrapperspb.StringValue](utils.Some(dir), "test")
	require.NoError(t, err)
	require.NoError(t, w.Persist(wrapperspb.String("hello")))

	bz, err := os.ReadFile(filepath.Join(dir, "test"+suffixA))
	require.NoError(t, err)
	require.True(t, len(bz) >= headerSize)

	// Verify CRC covers the payload (seq + proto data).
	wantCRC := crc32.Checksum(bz[crcSize:], crc32c)
	gotCRC := binary.BigEndian.Uint32(bz[:crcSize])
	require.Equal(t, wantCRC, gotCRC)

	// Verify seq.
	seq := binary.LittleEndian.Uint64(bz[crcSize : crcSize+seqSize])
	require.Equal(t, uint64(1), seq)
}
