package wal

import (
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
)

func dump(l *Log) [][]byte {
	var entries [][]byte
	for offset := l.MinOffset(); offset <= 0; offset++ {
		entries = append(entries, utils.OrPanic1(l.ReadFile(offset))...)
	}
	return entries
}

func TestReadAfterAppend(t *testing.T) {
	headPath := path.Join(t.TempDir(), "testlog")
	cfg := &Config{}
	entry := []byte{25}
	l := utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	// Append minimal amount of data.
	require.NoError(t, l.Append(entry))
	// dump the log - the written entry should already be there.
	require.NoError(t, utils.TestDiff(utils.Slice(entry), dump(l)))
}

func TestAppendRead(t *testing.T) {
	for _, reopen := range utils.Slice(true, false) {
		t.Run(fmt.Sprintf("reopen=%v", reopen), func(t *testing.T) {
			rng := utils.TestRng()
			headPath := path.Join(t.TempDir(), "testlog")
			cfg := &Config{FileSizeLimit: 1000}
			var want [][]byte
			t.Logf("Open a log")
			l := utils.OrPanic1(OpenLog(headPath, cfg))
			// Wrapped defer, since we assign to l multiple times.
			defer func() { l.Close() }()

			for it := range 5 {
				t.Logf("ITERATION %v", it)
				if reopen {
					l.Close()
					l = utils.OrPanic1(OpenLog(headPath, cfg))
				}
				t.Logf("Opening a log again should fail - previous instance holds a lock on it.")
				_, err := OpenLog(headPath, cfg)
				require.Error(t, err)
				t.Logf("Append a bunch of random entries.")
				for range 400 {
					entry := utils.GenBytes(rng, rng.Intn(50)+10)
					want = append(want, entry)
					require.NoError(t, l.Append(entry))
				}
				t.Logf("Sync the log and close.")
				require.NoError(t, l.Sync())

				t.Logf("Read entries.")
				if reopen {
					l.Close()
					l = utils.OrPanic1(OpenLog(headPath, cfg))
				}
				require.NoError(t, utils.TestDiff(want, dump(l)))
			}
		})
	}
}

func TestNoSync(t *testing.T) {
	rng := utils.TestRng()
	headPath := path.Join(t.TempDir(), "testlog")
	cfg := &Config{FileSizeLimit: 1000}

	l := utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	// Insert entries and sync in the middle.
	var want [][]byte
	syncEntries := 50
	for i := range syncEntries + 20 {
		if i == syncEntries {
			require.NoError(t, l.Sync())
		}
		entry := utils.GenBytes(rng, rng.Intn(50)+10)
		want = append(want, entry)
		require.NoError(t, l.Append(entry))
	}
	l.Close()

	// Read Entries - expect entries at least to the sync point.
	l = utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	got := dump(l)
	require.True(t, len(got) >= syncEntries)
	require.NoError(t, utils.TestDiff(want[:len(got)], got))
}

func TestTruncation(t *testing.T) {
	rng := utils.TestRng()
	headPath := path.Join(t.TempDir(), "testlog")
	cfg := &Config{FileSizeLimit: 1000}

	// Insert entries.
	l := utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	var want [][]byte
	for range 100 {
		entry := utils.GenBytes(rng, rng.Intn(50)+10)
		want = append(want, entry)
		require.NoError(t, l.Append(entry))
	}
	require.NoError(t, l.Sync())
	l.Close()

	// Truncate the head file.
	fi, err := os.Stat(headPath)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(headPath, fi.Size()/2))

	// Read Entries - expect a prefix.
	l = utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	got := dump(l)
	require.NoError(t, utils.TestDiff(want[:len(got)], got))
}

func TestSizeLimitsAndOffsets(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	baseName := "testlog"
	headPath := path.Join(dir, baseName)
	cfg := &Config{FileSizeLimit: 100, TotalSizeLimit: 3000}

	// Populate the log.
	l := utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	minEntrySize := int64(10)
	maxEntrySize := int64(20)
	entryCount := int64(500)
	// Pruning only happens after head rotation. Therefore to trigger pruning
	// we need to produce TotalSizeLimit bytes + whatever fits into the head (FileSizeLimit).
	// This is an over-estimation, since in reality each entry also contributes a couple of header bytes.
	require.True(t, cfg.TotalSizeLimit+cfg.FileSizeLimit < minEntrySize*entryCount)
	var want [][]byte
	for range entryCount {
		entry := utils.GenBytes(rng, int(rng.Int63n(maxEntrySize-minEntrySize)+minEntrySize))
		want = append(want, entry)
		require.NoError(t, l.Append(entry))
	}
	require.NoError(t, l.Sync())
	l.Close()

	// Verify file sizes.
	dirEntries, err := os.ReadDir(dir)
	require.NoError(t, err)
	total := int64(0)
	for _, e := range dirEntries {
		if !strings.HasPrefix(e.Name(), baseName) {
			continue
		}
		fi, err := os.Stat(path.Join(dir, e.Name()))
		require.NoError(t, err)
		require.True(t, fi.Size() < cfg.FileSizeLimit+maxEntrySize+headerSize)
		total += fi.Size()
	}
	require.True(t, total <= cfg.TotalSizeLimit+cfg.FileSizeLimit)
	require.True(t, total >= cfg.TotalSizeLimit-cfg.FileSizeLimit-maxEntrySize-headerSize)

	// Read the log, expect a suffix of entries.
	l = utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	got := dump(l)
	require.NoError(t, utils.TestDiff(want[len(want)-len(got):], got))
}

// WARNING: this benchmark is executed against tmp dir anyway,
// so most likely in RAM FS.
func BenchmarkAppendSync(b *testing.B) {
	rng := utils.TestRng()
	headPath := path.Join(b.TempDir(), "testlog")
	cfg := &Config{}

	var entries [][]byte
	for range 10000 {
		entries = append(entries, utils.GenBytes(rng, rng.Intn(100)+1000))
	}
	l := utils.OrPanic1(OpenLog(headPath, cfg))
	defer l.Close()
	for i := 0; b.Loop(); i++ {
		utils.OrPanic(l.Append(entries[i%len(entries)]))
		utils.OrPanic(l.Sync())
	}
}
