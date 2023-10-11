package memiavl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/tidwall/gjson"
	"github.com/tidwall/wal"
)

// OpenWAL opens the write ahead log, try to truncate the corrupted tail if there's any
// TODO fix in upstream: https://github.com/tidwall/wal/pull/22
func OpenWAL(dir string, opts *wal.Options) (*wal.Log, error) {
	log, err := wal.Open(dir, opts)
	if err == wal.ErrCorrupt {
		// try to truncate corrupted tail
		var fis []os.DirEntry
		fis, err = os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read wal dir fail: %w", err)
		}
		var lastSeg string
		for _, fi := range fis {
			if fi.IsDir() || len(fi.Name()) < 20 {
				continue
			}
			lastSeg = fi.Name()
		}

		if len(lastSeg) == 0 {
			return nil, err
		}
		if err = truncateCorruptedTail(filepath.Join(dir, lastSeg), opts.LogFormat); err != nil {
			return nil, fmt.Errorf("truncate corrupted tail fail: %w", err)
		}

		// try again
		return wal.Open(dir, opts)
	}

	return log, err
}

func truncateCorruptedTail(path string, format wal.LogFormat) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var pos int
	for len(data) > 0 {
		var n int
		if format == wal.JSON {
			n, err = loadNextJSONEntry(data)
		} else {
			n, err = loadNextBinaryEntry(data)
		}
		if err == wal.ErrCorrupt {
			break
		}
		if err != nil {
			return err
		}
		data = data[n:]
		pos += n
	}
	if pos != len(data) {
		return os.Truncate(path, int64(pos))
	}
	return nil
}

func loadNextJSONEntry(data []byte) (n int, err error) {
	// {"index":number,"data":string}
	idx := bytes.IndexByte(data, '\n')
	if idx == -1 {
		return 0, wal.ErrCorrupt
	}
	line := data[:idx]
	dres := gjson.Get(*(*string)(unsafe.Pointer(&line)), "data")
	if dres.Type != gjson.String {
		return 0, wal.ErrCorrupt
	}
	return idx + 1, nil
}

func loadNextBinaryEntry(data []byte) (n int, err error) {
	// data_size + data
	size, n := binary.Uvarint(data)
	if n <= 0 {
		return 0, wal.ErrCorrupt
	}
	if uint64(len(data)-n) < size {
		return 0, wal.ErrCorrupt
	}
	return n + int(size), nil
}
