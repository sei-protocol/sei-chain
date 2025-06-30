package changelog

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/cosmos/iavl"
	"github.com/tidwall/gjson"
	"github.com/tidwall/wal"
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
	defer rlog.Close()
	return rlog.LastIndex()
}

// truncateCorruptedTail truncates the corrupted tail
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

// loadNextJSONEntry loads json data like {"index":number,"data":string}
func loadNextJSONEntry(data []byte) (n int, err error) {
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

// loadNextBinaryEntry loads binary data like data_size + data
func loadNextBinaryEntry(data []byte) (n int, err error) {
	size, n := binary.Uvarint(data)
	if n <= 0 {
		return 0, wal.ErrCorrupt
	}
	if uint64(len(data)-n) < size {
		return 0, wal.ErrCorrupt
	}
	return n + int(size), nil
}

func channelBatchRecv[T any](ch <-chan *T) []*T {
	// block if channel is empty
	item := <-ch
	if item == nil {
		// channel is closed
		return nil
	}

	remaining := len(ch)
	result := make([]*T, 0, remaining+1)
	result = append(result, item)
	for i := 0; i < remaining; i++ {
		result = append(result, <-ch)
	}
	return result
}

func MockKVPairs(kvPairs ...string) []*iavl.KVPair {
	result := make([]*iavl.KVPair, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		result[i/2] = &iavl.KVPair{
			Key:   []byte(kvPairs[i]),
			Value: []byte(kvPairs[i+1]),
		}
	}
	return result
}
