package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/tidwall/gjson"
	"github.com/tidwall/wal"

	seidbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

func LogPath(dir string) string {
	return filepath.Join(dir, "changelog")
}

// GetLastIndex returns the last written index of the replay log
func GetLastIndex(dir string) (index uint64, err error) {
	rlog, err := open(dir, &wal.Options{
		NoSync: true,
		NoCopy: true,
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = rlog.Close() }()
	return rlog.LastIndex()
}

// ErrCorrupt reports a log that cannot be read without repair.
var ErrCorrupt = wal.ErrCorrupt

// segmentNameLen is the length of a log segment file name.
const segmentNameLen = 20

// VerifyIntact reports whether the binary log in dir can be opened without
// repair. It returns ErrCorrupt when the tail segment ends mid-record or an
// interrupted truncation is still in progress, and never modifies dir.
//
// A reader on a live node calls this before it opens the log, because open
// repairs what it finds: truncateCorruptedTail cuts a torn tail, and tidwall
// completes an interrupted truncation by renaming or removing segments. On a
// live node a torn tail is usually a write in progress rather than lasting
// damage, so the caller reruns instead of repairing.
func VerifyIntact(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read wal dir %s: %w", dir, err)
	}

	// os.ReadDir sorts by name, and segment names are zero-padded, so the last
	// match is the tail segment open would truncate.
	var tail string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < segmentNameLen {
			continue
		}
		if strings.HasSuffix(name, ".START") || strings.HasSuffix(name, ".END") {
			return fmt.Errorf("%w: truncation marker %s is present in %s", ErrCorrupt, name, dir)
		}
		tail = name
	}
	if tail == "" {
		return nil
	}

	path := filepath.Join(dir, tail)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read wal segment %s: %w", path, err)
	}
	for pos := 0; pos < len(data); {
		n, err := loadNextBinaryEntry(data[pos:])
		if err != nil {
			return fmt.Errorf("%w: segment %s ends mid-record at offset %d", ErrCorrupt, path, pos)
		}
		pos += n
	}
	return nil
}

// truncateCorruptedTail truncates the corrupted tail
func truncateCorruptedTail(path string, format wal.LogFormat) error {
	data, err := os.ReadFile(filepath.Clean(path))
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
		if errors.Is(err, wal.ErrCorrupt) {
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

// loadNextJSONEntry loads json data like {"index":number,"data":string}
func loadNextJSONEntry(data []byte) (n int, err error) {
	idx := bytes.IndexByte(data, '\n')
	if idx == -1 {
		return 0, wal.ErrCorrupt
	}
	line := data[:idx]
	dres := gjson.Get(*(*string)(unsafe.Pointer(&line)), "data") //nolint:gosec
	if dres.Type != gjson.String {
		return 0, wal.ErrCorrupt
	}
	return idx + 1, nil
}

// loadNextBinaryEntry loads binary data like data_size + data
func loadNextBinaryEntry(data []byte) (n int, err error) {
	s, n := binary.Uvarint(data)
	if n <= 0 {
		return 0, wal.ErrCorrupt
	}
	if s > math.MaxInt32 {
		return 0, wal.ErrCorrupt
	}
	size := int(s)
	if len(data)-n < size {
		return 0, wal.ErrCorrupt
	}
	return n + size, nil
}

func channelBatchRecv[T any](ch <-chan T) []T {
	// block if channel is empty
	item, ok := <-ch
	if !ok {
		// channel is closed
		return nil
	}

	remaining := len(ch)
	result := make([]T, 0, remaining+1)
	result = append(result, item)
	for i := 0; i < remaining; i++ {
		result = append(result, <-ch)
	}
	return result
}

func MockKVPairs(kvPairs ...string) []*seidbproto.KVPair {
	result := make([]*seidbproto.KVPair, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		result[i/2] = &seidbproto.KVPair{
			Key:   []byte(kvPairs[i]),
			Value: []byte(kvPairs[i+1]),
		}
	}
	return result
}
