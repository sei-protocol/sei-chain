package seiwal

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGenericWALQueueDepthSampler exercises the queue-depth samplers (both the serializing layer's and the
// inner byte engine's) on a tiny interval, validating concurrent sampling under the race detector and a clean
// shutdown on Close.
func TestGenericWALQueueDepthSampler(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.MetricsSampleInterval = time.Millisecond
	w := openStringWAL(t, cfg)
	for i := uint64(1); i <= 300; i++ {
		require.NoError(t, w.Append(i, fmt.Sprintf("v%d", i)))
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())
}

func stringSerialize(s string) ([]byte, error)   { return []byte(s), nil }
func stringDeserialize(b []byte) (string, error) { return string(b), nil }

func openStringWAL(t *testing.T, cfg *Config) WAL[string] {
	t.Helper()
	w, err := NewGenericWAL[string](cfg, stringSerialize, stringDeserialize)
	require.NoError(t, err)
	return w
}

type indexedString struct {
	index uint64
	value string
}

func collectStrings(t *testing.T, w WAL[string], start uint64, end uint64) []indexedString {
	t.Helper()
	it, err := w.Iterator(start, end)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var out []indexedString
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		index, value := it.Entry()
		out = append(out, indexedString{index: index, value: value})
	}
	return out
}

// TestSerializeFailureClosesInnerWAL verifies that a serialize error tears the inner byte WAL down instead of
// orphaning its writer goroutine and mutable-file handle. The inner WAL is healthy (only serialization failed),
// so fail() closes it gracefully — observable as the inner mutable file being sealed.
func TestSerializeFailureClosesInnerWAL(t *testing.T) {
	cfg := testConfig(t.TempDir())
	boom := errors.New("serialize boom")
	serialize := func(s string) ([]byte, error) { return nil, boom }
	w, err := NewGenericWAL[string](cfg, serialize, stringDeserialize)
	require.NoError(t, err)

	require.NoError(t, w.Append(1, "one")) // scheduling succeeds; serialize fails on the serializer goroutine

	// Close drains the serializer goroutine, which by now has run fail() -> inner.Close().
	err = w.Close()
	require.Error(t, err)
	require.ErrorIs(t, err, boom)

	sw := w.(*serializingWAL[string])
	inner := sw.inner.(*walImpl)
	require.True(t, inner.mutableFile.sealed) // inner cleanly closed by fail(), not orphaned
}

// TestGenericWALFlushIOFailureBricksWAL verifies that an inner flush IO failure bricks the serializing WAL too:
// the inner byte engine tears itself down, and the serializing layer mirrors that rather than delegating
// subsequent appends to a dead inner WAL.
func TestGenericWALFlushIOFailureBricksWAL(t *testing.T) {
	cfg := testConfig(t.TempDir())
	w := openStringWAL(t, cfg)

	ser, ok := w.(*serializingWAL[string])
	require.True(t, ok)
	inner, ok := ser.inner.(*walImpl)
	require.True(t, ok)

	// Close the inner mutable file's descriptor so the flush the inner engine performs fails.
	require.NoError(t, inner.mutableFile.file.Close())

	require.NoError(t, w.Append(1, "one"))
	require.Error(t, w.Flush(), "flush must surface the inner IO failure")

	// Bricking cancels the serializing layer's context; wait for it so the assertions below are deterministic.
	select {
	case <-ser.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("serializing WAL did not brick after flush failure")
	}

	require.Error(t, w.Append(2, "two"), "appends must fail on a bricked WAL")
	require.Error(t, w.Flush(), "flush must fail on a bricked WAL")
	require.Error(t, w.Close(), "Close must surface the fatal flush error")
}

func TestGenericWALRoundTrip(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.PermitGaps = true
	w := openStringWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	require.NoError(t, w.Append(1, "one"))
	require.NoError(t, w.Append(2, "two"))
	require.NoError(t, w.Append(5, "five")) // non-contiguous index is allowed when gaps are permitted
	require.NoError(t, w.Flush())

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)

	require.Equal(t, []indexedString{{1, "one"}, {2, "two"}, {5, "five"}}, collectStrings(t, w, 0, 5))
	require.Equal(t, []indexedString{{2, "two"}, {5, "five"}}, collectStrings(t, w, 2, 5))
}

func TestGenericWALReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openStringWAL(t, cfg)
	for i := uint64(1); i <= 3; i++ {
		require.NoError(t, w.Append(i, fmt.Sprintf("v%d", i)))
	}
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	w2 := openStringWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, first, last, err := w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(3), last)
	require.Equal(t, []indexedString{{1, "v1"}, {2, "v2"}, {3, "v3"}}, collectStrings(t, w2, 0, 3))
}

func TestGenericWALPrune(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.TargetFileSize = 1 // one record per file so pruning drops whole files

	w := openStringWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for i := uint64(1); i <= 10; i++ {
		require.NoError(t, w.Append(i, fmt.Sprintf("v%d", i)))
	}
	require.NoError(t, w.Flush())

	require.NoError(t, w.PruneBefore(5))

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), first)
	require.Equal(t, uint64(10), last)
}

func TestGenericWALRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1

	w := openStringWAL(t, cfg)
	for i := uint64(1); i <= 6; i++ {
		require.NoError(t, w.Append(i, fmt.Sprintf("v%d", i)))
	}
	require.NoError(t, w.Close())

	require.NoError(t, PruneAfter(cfg.Path, 3))
	w2 := openStringWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, first, last, err := w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(3), last)
	require.Equal(t, []indexedString{{1, "v1"}, {2, "v2"}, {3, "v3"}}, collectStrings(t, w2, 0, 3))
}

func TestGenericWALSerializeErrorSurfaces(t *testing.T) {
	serErr := errors.New("serialize boom")
	serialize := func(s string) ([]byte, error) {
		if s == "bad" {
			return nil, serErr
		}
		return []byte(s), nil
	}
	w, err := NewGenericWAL[string](testConfig(t.TempDir()), serialize, stringDeserialize)
	require.NoError(t, err)

	require.NoError(t, w.Append(1, "good"))
	require.NoError(t, w.Append(2, "bad")) // async; the serialize failure tears the pipeline down

	// The fatal serialization error surfaces on the next synchronous operation.
	require.Error(t, w.Flush())
	require.Error(t, w.Close())
}

func TestGenericWALDeserializeErrorSurfaces(t *testing.T) {
	deErr := errors.New("deserialize boom")
	deserialize := func(b []byte) (string, error) {
		if string(b) == "poison" {
			return "", deErr
		}
		return string(b), nil
	}
	w, err := NewGenericWAL[string](testConfig(t.TempDir()), stringSerialize, deserialize)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()

	require.NoError(t, w.Append(1, "ok"))
	require.NoError(t, w.Append(2, "poison"))
	require.NoError(t, w.Flush())

	it, err := w.Iterator(0, 2)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	index, value := it.Entry()
	require.Equal(t, uint64(1), index)
	require.Equal(t, "ok", value)

	// The poison record fails to deserialize; the error surfaces from Next, not a clean EOF.
	ok, err = it.Next()
	require.Error(t, err)
	require.False(t, ok)
}

func TestGenericWALAppendOrdering(t *testing.T) {
	w := openStringWAL(t, testConfig(t.TempDir()))

	require.NoError(t, w.Append(5, "five"))
	require.NoError(t, w.Flush())
	// The inner byte engine enforces strictly-increasing indices; a stale index tears the pipeline down.
	require.NoError(t, w.Append(4, "four")) // async, will fail on the goroutine
	require.Error(t, w.Flush())
	// Close surfaces the same fatal error rather than succeeding.
	require.Error(t, w.Close())
}

// faultyInner is a WAL[[]byte] whose Bounds/Iterator return an injected error. An inner WAL only errors on
// those read-path calls once it is already dead, so it models a failed inner engine for the serializing layer.
type faultyInner struct {
	boundsErr error
	iterErr   error
}

var _ WAL[[]byte] = (*faultyInner)(nil)

func (f *faultyInner) Append(uint64, []byte) error           { return nil }
func (f *faultyInner) Flush() error                          { return nil }
func (f *faultyInner) Bounds() (bool, uint64, uint64, error) { return false, 0, 0, f.boundsErr }
func (f *faultyInner) PruneBefore(uint64) error              { return nil }
func (f *faultyInner) Iterator(uint64, uint64) (Iterator[[]byte], error) {
	return nil, f.iterErr
}
func (f *faultyInner) Close() error { return nil }

// TestGenericWALBricksOnInnerBoundsError verifies that an error from the inner WAL's Bounds — which only
// happens once the inner engine is already dead — bricks the serializing layer instead of leaving it running
// against a dead inner until a later mutating call fails.
func TestGenericWALBricksOnInnerBoundsError(t *testing.T) {
	boom := errors.New("inner bounds boom")
	cfg := testConfig(t.TempDir())
	cfg.MetricsSampleInterval = 0
	s := newSerializingWAL[string](cfg, &faultyInner{boundsErr: boom}, stringSerialize, stringDeserialize)
	defer func() { _ = s.Close() }()

	_, _, _, err := s.Bounds()
	require.Error(t, err)
	require.ErrorIs(t, err, boom)

	select {
	case <-s.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("serializing WAL did not brick after inner Bounds error")
	}
	require.ErrorIs(t, s.asyncError(), boom)
	require.Error(t, s.Append(1, "x"), "appends must fail on a bricked WAL")
}

// TestGenericWALBricksOnInnerIteratorError is the Iterator analogue of TestGenericWALBricksOnInnerBoundsError.
func TestGenericWALBricksOnInnerIteratorError(t *testing.T) {
	boom := errors.New("inner iterator boom")
	cfg := testConfig(t.TempDir())
	cfg.MetricsSampleInterval = 0
	s := newSerializingWAL[string](cfg, &faultyInner{iterErr: boom}, stringSerialize, stringDeserialize)
	defer func() { _ = s.Close() }()

	_, err := s.Iterator(0, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, boom)

	select {
	case <-s.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("serializing WAL did not brick after inner Iterator error")
	}
	require.ErrorIs(t, s.asyncError(), boom)
	require.Error(t, s.Append(1, "x"), "appends must fail on a bricked WAL")
}
