package rlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/sei-protocol/sei-db/proto"
	"github.com/tidwall/gjson"
	"github.com/tidwall/wal"
)

type Manager struct {
	rlog         *wal.Log
	writeChannel chan *LogEntry
	quitSignal   chan error
	config       Config
}

type Config struct {
	Fsync            bool
	ZeroCopy         bool
	WriteChannelSize int
}

type LogEntry struct {
	Index uint64
	Data  proto.ReplayLogEntry
}

func NewManager(dir string, config Config) (*Manager, error) {
	rlog, err := OpenRlog(dir, &wal.Options{NoCopy: config.ZeroCopy, NoSync: !config.Fsync})
	if err != nil {
		return nil, err
	}
	manager := &Manager{rlog: rlog, config: config}
	if config.WriteChannelSize > 0 {
		// async write is enabled

		go manager.startAsyncWrite()
	}

	return manager, nil
}

// OpenRlog opens the replay log, try to truncate the corrupted tail if there's any
func OpenRlog(dir string, opts *wal.Options) (*wal.Log, error) {
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

// LastIndex returns the last written index of the replay log
func (m *Manager) LastIndex() (index uint64, err error) {
	return m.rlog.LastIndex()
}

func (m *Manager) Replay(start uint64, end uint64, processFn func(index uint64, entry proto.ReplayLogEntry) error) error {
	for i := start; i <= end; i++ {
		var entry proto.ReplayLogEntry
		bz, err := m.rlog.Read(i)
		if err != nil {
			return fmt.Errorf("read rlog failed, %w", err)
		}
		if err := entry.Unmarshal(bz); err != nil {
			return fmt.Errorf("unmarshal rlog failed, %w", err)
		}
		err = processFn(i, entry)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Write(entry LogEntry) error {
	if m.config.WriteChannelSize > 0 && m.writeChannel != nil {
		// async write
		m.writeChannel <- &entry
	} else {
		// synchronous write
		bz, err := entry.Data.Marshal()
		if err != nil {
			return err
		}
		if err := m.rlog.Write(entry.Index, bz); err != nil {
			return err
		}
	}
	return nil
}

// TruncateAfter will remove all entries that are after the provided `index`.
// In other words the entry at `index` becomes the last entry in the log.
func (m *Manager) TruncateAfter(index uint64) error {
	return m.rlog.TruncateBack(index)
}

// TruncateFront will remove all entries that are before the provided `index`.
// In other words the entry at `index` becomes the first entry in the log.
func (m *Manager) TruncateFront(index uint64) error {
	return m.rlog.TruncateFront(index)
}

// Close will close the underline replay log file
func (m *Manager) Close() error {
	return m.rlog.Close()
}

func (m *Manager) startAsyncWrite() {
	m.writeChannel = make(chan *LogEntry, m.config.WriteChannelSize)
	m.quitSignal = make(chan error)
	batch := wal.Batch{}
	defer close(m.quitSignal)
	for {
		entries := channelBatchRecv(m.writeChannel)
		if len(entries) == 0 {
			// channel is closed
			break
		}

		for _, entry := range entries {
			bz, err := entry.Data.Marshal()
			if err != nil {
				m.quitSignal <- err
				return
			}
			batch.Write(entry.Index, bz)
		}

		if err := m.rlog.WriteBatch(&batch); err != nil {
			m.quitSignal <- err
			return
		}
		batch.Clear()
	}
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

// checkAsyncCommit check the quit signal of async rlog writing
func (m *Manager) CheckAsyncCommit() error {
	select {
	case err := <-m.quitSignal:
		// async wal writing failed, we need to abort the state machine
		return fmt.Errorf("async wal writing goroutine quit unexpectedly: %w", err)
	default:
	}
	return nil
}

func (m *Manager) WaitAsyncCommit() error {
	if m.writeChannel == nil {
		return nil
	}
	close(m.writeChannel)
	err := <-m.quitSignal
	m.writeChannel = nil
	m.quitSignal = nil
	return err
}
