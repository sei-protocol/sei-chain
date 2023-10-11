package memiavl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/wal"
)

func TestCorruptedTail(t *testing.T) {
	opts := &wal.Options{
		LogFormat: wal.JSON,
	}
	dir := t.TempDir()

	testCases := []struct {
		name      string
		logs      []byte
		lastIndex uint64
	}{
		{"failure-1", []byte("\n"), 0},
		{"failure-2", []byte(`{}` + "\n"), 0},
		{"failure-3", []byte(`{"index":"1"}` + "\n"), 0},
		{"failure-4", []byte(`{"index":"1","data":"?"}`), 0},
		{"failure-5", []byte(`{"index":1,"data":"?"}` + "\n" + `{"index":"1","data":"?"}`), 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.WriteFile(filepath.Join(dir, "00000000000000000001"), tc.logs, 0o600)

			_, err := wal.Open(dir, opts)
			require.Equal(t, wal.ErrCorrupt, err)

			log, err := OpenWAL(dir, opts)
			require.NoError(t, err)

			lastIndex, err := log.LastIndex()
			require.NoError(t, err)
			require.Equal(t, tc.lastIndex, lastIndex)
		})
	}
}
