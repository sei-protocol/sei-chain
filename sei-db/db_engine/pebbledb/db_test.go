package pebbledb

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
)

func TestDBGetSetDelete(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	key := []byte("k1")
	val := []byte("v1")

	_, c, err := db.Get(key)
	if err != db_engine.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v (closer=%v)", err, c)
	}

	if err := db.Set(key, val, db_engine.WriteOptions{Sync: false}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, closer, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if closer == nil {
		t.Fatalf("Get returned nil closer")
	}
	cloned := bytes.Clone(got)
	_ = closer.Close()

	if !bytes.Equal(cloned, val) {
		t.Fatalf("value mismatch: got %q want %q", cloned, val)
	}

	if err := db.Delete(key, db_engine.WriteOptions{Sync: false}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, c2, err := db.Get(key)
	if err != db_engine.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v (closer=%v)", err, c2)
	}
}

func TestBatchAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	b := db.NewBatch()
	defer func() { _ = b.Close() }()

	if err := b.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatalf("batch set: %v", err)
	}
	if err := b.Set([]byte("b"), []byte("2")); err != nil {
		t.Fatalf("batch set: %v", err)
	}

	if err := b.Commit(db_engine.WriteOptions{Sync: false}); err != nil {
		t.Fatalf("batch commit: %v", err)
	}

	for _, tc := range []struct {
		k string
		v string
	}{
		{"a", "1"},
		{"b", "2"},
	} {
		got, c, err := db.Get([]byte(tc.k))
		if err != nil {
			t.Fatalf("Get(%q): %v", tc.k, err)
		}
		cloned := bytes.Clone(got)
		_ = c.Close()
		if string(cloned) != tc.v {
			t.Fatalf("Get(%q)=%q want %q", tc.k, cloned, tc.v)
		}
	}
}

func TestIteratorBounds(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Keys: a, b, c
	for _, k := range []string{"a", "b", "c"} {
		if err := db.Set([]byte(k), []byte("x"), db_engine.WriteOptions{Sync: false}); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	itr, err := db.NewIter(&db_engine.IterOptions{LowerBound: []byte("b"), UpperBound: []byte("d")})
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	defer func() { _ = itr.Close() }()

	var keys []string
	for ok := itr.First(); ok && itr.Valid(); ok = itr.Next() {
		keys = append(keys, string(itr.Key()))
	}
	if err := itr.Error(); err != nil {
		t.Fatalf("iter error: %v", err)
	}
	// LowerBound inclusive => includes b; UpperBound exclusive => includes c (d not present anyway)
	if len(keys) != 2 || keys[0] != "b" || keys[1] != "c" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestIteratorPrev(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Keys: a, b, c
	for _, k := range []string{"a", "b", "c"} {
		if err := db.Set([]byte(k), []byte("x"), db_engine.WriteOptions{Sync: false}); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	itr, err := db.NewIter(nil)
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	defer func() { _ = itr.Close() }()

	if !itr.Last() || !itr.Valid() {
		t.Fatalf("expected Last() to position iterator")
	}
	if string(itr.Key()) != "c" {
		t.Fatalf("expected key=c at Last(), got %q", itr.Key())
	}

	if !itr.Prev() || !itr.Valid() {
		t.Fatalf("expected Prev() to succeed")
	}
	if string(itr.Key()) != "b" {
		t.Fatalf("expected key=b after Prev(), got %q", itr.Key())
	}
}

func TestIteratorNextPrefixWithComparerSplit(t *testing.T) {
	// Use a custom comparer with Split that treats everything up to (and including) '/'
	// as the "prefix" for NextPrefix() / prefix-based skipping.
	cmp := *pebble.DefaultComparer
	cmp.Name = "sei-db/test-split-on-slash"
	cmp.Split = func(k []byte) int {
		for i, b := range k {
			if b == '/' {
				return i + 1
			}
		}
		return len(k)
	}
	// NextPrefix relies on Comparer.ImmediateSuccessor to compute a key that is
	// guaranteed to be greater than all keys sharing the current prefix.
	// pebble.DefaultComparer.ImmediateSuccessor appends 0x00, which is not
	// sufficient for our "prefix ends at '/'" convention (e.g. "a/\x00" < "a/2").
	// We provide an ImmediateSuccessor that increments the last byte (from the end)
	// to produce a prefix upper bound (e.g. "a/" -> "a0").
	cmp.ImmediateSuccessor = func(dst, a []byte) []byte {
		for i := len(a) - 1; i >= 0; i-- {
			if a[i] != 0xff {
				dst = append(dst, a[:i+1]...)
				dst[len(dst)-1]++
				return dst
			}
		}
		return append(dst, a...)
	}

	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{Comparer: &cmp})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, k := range []string{"a/1", "a/2", "a/3", "b/1"} {
		if err := db.Set([]byte(k), []byte("x"), db_engine.WriteOptions{Sync: false}); err != nil {
			t.Fatalf("Set(%q): %v", k, err)
		}
	}

	itr, err := db.NewIter(nil)
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	defer func() { _ = itr.Close() }()

	if !itr.SeekGE([]byte("a/")) || !itr.Valid() {
		t.Fatalf("expected SeekGE(a/) to be valid")
	}
	if !bytes.HasPrefix(itr.Key(), []byte("a/")) {
		t.Fatalf("expected key with prefix a/, got %q", itr.Key())
	}

	if !itr.NextPrefix() || !itr.Valid() {
		t.Fatalf("expected NextPrefix() to move to next prefix")
	}
	if string(itr.Key()) != "b/1" {
		t.Fatalf("expected key=b/1 after NextPrefix(), got %q", itr.Key())
	}
}

func TestOpenOptionsComparerTypeCheck(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir, db_engine.OpenOptions{Comparer: "not-a-pebble-comparer"})
	if err == nil {
		t.Fatalf("expected error for invalid comparer type")
	}
}

func TestErrNotFoundConsistency(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Test that Get on missing key returns ErrNotFound
	_, closer, err := db.Get([]byte("missing-key"))
	if err == nil {
		if closer != nil {
			_ = closer.Close()
		}
		t.Fatalf("expected error for missing key")
	}

	// Test that error is ErrNotFound
	if err != db_engine.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Test that IsNotFound helper works
	if !db_engine.IsNotFound(err) {
		t.Fatalf("IsNotFound should return true for ErrNotFound")
	}
}

func TestGetCloserMustWork(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set multiple keys
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		val := []byte(fmt.Sprintf("val-%d", i))
		if err := db.Set(key, val, db_engine.WriteOptions{Sync: false}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// Get keys and ensure closers work properly
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		val, closer, err := db.Get(key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if closer == nil {
			t.Fatalf("Get returned nil closer")
		}

		// Must clone before closing
		cloned := bytes.Clone(val)
		if err := closer.Close(); err != nil {
			t.Fatalf("closer.Close: %v", err)
		}

		expected := fmt.Sprintf("val-%d", i)
		if string(cloned) != expected {
			t.Fatalf("value mismatch: got %q want %q", cloned, expected)
		}
	}
}

func TestBatchLenResetDelete(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// First, set a key so we can delete it
	if err := db.Set([]byte("to-delete"), []byte("val"), db_engine.WriteOptions{Sync: false}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	b := db.NewBatch()
	defer func() { _ = b.Close() }()

	// Record initial batch len (Pebble batch always has a header, so may not be 0)
	initialLen := b.Len()

	// Add some operations
	if err := b.Set([]byte("a"), []byte("1")); err != nil {
		t.Fatalf("batch set: %v", err)
	}
	if err := b.Delete([]byte("to-delete")); err != nil {
		t.Fatalf("batch delete: %v", err)
	}

	// Len should increase after operations (Pebble Len() returns bytes, not count)
	if b.Len() <= initialLen {
		t.Fatalf("expected Len() to increase after operations, got %d (initial %d)", b.Len(), initialLen)
	}

	// Reset should clear the batch back to initial state
	b.Reset()
	if b.Len() != initialLen {
		t.Fatalf("expected Len()=%d after Reset, got %d", initialLen, b.Len())
	}

	// Add and commit
	if err := b.Set([]byte("b"), []byte("2")); err != nil {
		t.Fatalf("batch set: %v", err)
	}
	if err := b.Commit(db_engine.WriteOptions{Sync: false}); err != nil {
		t.Fatalf("batch commit: %v", err)
	}

	// Verify "b" was written
	got, closer, err := db.Get([]byte("b"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	cloned := bytes.Clone(got)
	_ = closer.Close()
	if string(cloned) != "2" {
		t.Fatalf("expected '2', got %q", cloned)
	}
}

func TestIteratorSeekLTAndValue(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert keys: a, b, c with values
	for _, kv := range []struct{ k, v string }{
		{"a", "val-a"},
		{"b", "val-b"},
		{"c", "val-c"},
	} {
		if err := db.Set([]byte(kv.k), []byte(kv.v), db_engine.WriteOptions{Sync: false}); err != nil {
			t.Fatalf("Set(%q): %v", kv.k, err)
		}
	}

	itr, err := db.NewIter(nil)
	if err != nil {
		t.Fatalf("NewIter: %v", err)
	}
	defer func() { _ = itr.Close() }()

	// SeekLT("c") should position at "b"
	if !itr.SeekLT([]byte("c")) || !itr.Valid() {
		t.Fatalf("expected SeekLT(c) to be valid")
	}
	if string(itr.Key()) != "b" {
		t.Fatalf("expected key=b after SeekLT(c), got %q", itr.Key())
	}
	if string(itr.Value()) != "val-b" {
		t.Fatalf("expected value=val-b, got %q", itr.Value())
	}
}

func TestFlush(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set some data
	if err := db.Set([]byte("flush-test"), []byte("val"), db_engine.WriteOptions{Sync: false}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Flush should succeed
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Data should still be readable
	got, closer, err := db.Get([]byte("flush-test"))
	if err != nil {
		t.Fatalf("Get after flush: %v", err)
	}
	cloned := bytes.Clone(got)
	_ = closer.Close()
	if string(cloned) != "val" {
		t.Fatalf("expected 'val', got %q", cloned)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, db_engine.OpenOptions{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// First close should succeed
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close should be idempotent (no panic, returns nil)
	if err := db.Close(); err != nil {
		t.Fatalf("second Close should return nil, got: %v", err)
	}
}
