package rlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/tidwall/gjson"
	"github.com/tidwall/wal"
)

type Config struct {
	DisableFsync    bool
	ZeroCopy        bool
	WriteBufferSize int
	ReadBufferSize  int
}

// Manager manages the replay log operations for reads and writes.
// Replay Log is an append-only log which persists all changesets.
type Manager struct {
	rlog   *wal.Log
	writer Writer
	reader Reader
}

func (m *Manager) Writer() Writer {
	return m.writer
}

func (m *Manager) Reader() Reader {
	return m.reader
}

func NewManager(logger logger.Logger, dir string, config Config) (*Manager, error) {
	rlog, err := openRlog(dir, &wal.Options{
		NoSync: config.DisableFsync,
		NoCopy: config.ZeroCopy,
	})
	if err != nil {
		return nil, err
	}
	writer, err := NewWriter(logger, rlog, config)
	if err != nil {
		return nil, err
	}
	reader, err := NewReader(logger, rlog, config)
	if err != nil {
		return nil, err
	}
	return &Manager{
		rlog:   rlog,
		writer: writer,
		reader: reader,
	}, nil
}

// LastIndex returns the last written index of the replay log
func (m *Manager) LastIndex() (index uint64, err error) {
	return m.rlog.LastIndex()
}

func (m *Manager) Close() error {
	errWriter := m.writer.WaitAsyncCommit()
	errReader := m.reader.StopSubscriber()
	errClose := m.rlog.Close()
	return utils.Join(errWriter, errReader, errClose)
}

// OpenRlog opens the replay log, try to truncate the corrupted tail if there's any
func openRlog(dir string, opts *wal.Options) (*wal.Log, error) {
	rlog, err := wal.Open(dir, opts)
	if errors.Is(err, wal.ErrCorrupt) {
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

	return rlog, err
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

// GetLastIndex returns the last written index of the replay log
func GetLastIndex(dir string) (index uint64, err error) {
	rlog, err := openRlog(dir, nil)
	if err != nil {
		return 0, err
	}
	defer rlog.Close()
	return rlog.LastIndex()
}
