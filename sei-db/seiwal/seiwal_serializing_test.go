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

func collectStrings(t *testing.T, w WAL[string], start uint64) []indexedString {
	t.Helper()
	it, err := w.Iterator(start)
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

func TestGenericWALRoundTrip(t *testing.T) {
	w := openStringWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	require.NoError(t, w.Append(1, "one"))
	require.NoError(t, w.Append(2, "two"))
	require.NoError(t, w.Append(5, "five")) // non-contiguous index is allowed
	require.NoError(t, w.Flush())

	ok, first, last, err := w.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(5), last)

	require.Equal(t, []indexedString{{1, "one"}, {2, "two"}, {5, "five"}}, collectStrings(t, w, 0))
	require.Equal(t, []indexedString{{2, "two"}, {5, "five"}}, collectStrings(t, w, 2))
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
	require.Equal(t, []indexedString{{1, "v1"}, {2, "v2"}, {3, "v3"}}, collectStrings(t, w2, 0))
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

	require.NoError(t, w.Prune(5))

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

	w2, err := NewGenericWALWithRollback[string](cfg, 3, stringSerialize, stringDeserialize)
	require.NoError(t, err)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, first, last, err := w2.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(3), last)
	require.Equal(t, []indexedString{{1, "v1"}, {2, "v2"}, {3, "v3"}}, collectStrings(t, w2, 0))
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

	it, err := w.Iterator(0)
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
