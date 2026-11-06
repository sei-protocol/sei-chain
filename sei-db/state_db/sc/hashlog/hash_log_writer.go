package hashlog

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// HashLogWriter appends whole blocks of named hashes to a hash log directory, and owns that directory's file
// rotation and garbage collection. No other writer may share the directory. Writes are performed by a background
// goroutine, in the order they are submitted. Every method is safe to call from any goroutine.
type HashLogWriter struct {

	// The writer's configuration, with Version sanitized to be filename-safe.
	config HashLoggerConfig

	// Requests for the writer goroutine, handled in order. Each is one of the *Request types. Never closed.
	requests chan any

	// Closed by the writer goroutine as it exits.
	stopped chan struct{}

	// Guarantees the Close() handshake runs at most once.
	closeOnce sync.Once

	// The failure that stopped the writer goroutine, or nil. Written by the goroutine before it closes stopped,
	// and read only after stopped is closed.
	failure error

	// Every field below is owned by the writer goroutine.

	// The file accepting writes, or nil if none is open.
	mutableFile *hashLogFile

	// Bookkeeping for sealed files, keyed by file index.
	sealedFiles map[uint64]*sealedFileInfo

	// The number of bytes used by sealed files.
	currentDiskSpaceUsed uint64

	// The lowest file index GC has not yet passed.
	lowestLogFileIndex uint64

	// The index of the next file opened.
	nextFileIndex uint64

	// The highest block number written so far.
	latestBlock uint64
}

// NewHashLogWriter opens a HashLogWriter on config.Path and starts its goroutine.
func NewHashLogWriter(config *HashLoggerConfig) (*HashLogWriter, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid hash log writer config: %w", err)
	}
	if err := util.EnsureDirectoryExists(config.Path, true); err != nil {
		return nil, fmt.Errorf("failed to ensure hash log directory %s: %w", config.Path, err)
	}
	w := &HashLogWriter{
		config:      *config,
		requests:    make(chan any, config.WriteBufferSize),
		stopped:     make(chan struct{}),
		sealedFiles: make(map[uint64]*sealedFileInfo),
	}
	w.config.Version = sanitizeVersion(config.Version)
	if err := w.scanDirectory(); err != nil {
		return nil, fmt.Errorf("failed to scan hash log directory %s: %w", config.Path, err)
	}
	go w.run()
	return w, nil
}

// Write submits one block's hashes, written as one row whose columns are the sorted hash names. A block whose
// names differ from the previous block's starts a new file. Rows are written in submission order. A name containing
// characters outside the legal allow-list is logged and dropped.
//
// The hashes map and its slices are read after Write returns, so the caller must not mutate them afterwards.
// Blocks while the writer is behind. Errors once the writer has stopped; a call concurrent with Close() may
// instead return nil without writing the block.
func (w *HashLogWriter) Write(blockNumber uint64, hashes map[string][]byte) error {
	if err := w.send(writeBlockRequest{blockNumber: blockNumber, hashes: hashes}); err != nil {
		return fmt.Errorf("failed to write the hashes of block %d: %w", blockNumber, err)
	}
	return nil
}

// Flush returns once every block submitted before it is written to disk and fsynced.
func (w *HashLogWriter) Flush() error {
	reply := make(chan error, 1)
	if err := w.send(flushRequest{reply: reply}); err != nil {
		return fmt.Errorf("failed to flush the hash log: %w", err)
	}
	select {
	case err := <-reply:
		if err != nil {
			return fmt.Errorf("failed to flush the hash log: %w", err)
		}
		return nil
	case <-w.stopped:
		return fmt.Errorf("failed to flush the hash log: %w", w.stoppedError())
	}
}

// Close writes every block submitted before it, seals the mutable file, and stops the writer. Returns the failure
// that stopped the writer, if any. Idempotent.
func (w *HashLogWriter) Close() error {
	w.closeOnce.Do(func() {
		select {
		case w.requests <- closeRequest{}:
		case <-w.stopped:
		}
	})
	<-w.stopped
	if w.failure != nil {
		return fmt.Errorf("hash log writer failed: %w", w.failure)
	}
	return nil
}

// Hands request to the writer goroutine, blocking while it is behind. Errors if it has stopped.
func (w *HashLogWriter) send(request any) error {
	// A stopped writer must refuse the request rather than race it into the buffer, where nothing would handle it.
	select {
	case <-w.stopped:
		return w.stoppedError()
	default:
	}
	select {
	case w.requests <- request:
		return nil
	case <-w.stopped:
		return w.stoppedError()
	}
}

// Returns the error a call made after the writer goroutine stopped returns. Valid only once stopped is closed.
func (w *HashLogWriter) stoppedError() error {
	if w.failure != nil {
		return fmt.Errorf("hash log writer stopped: %w", w.failure)
	}
	return fmt.Errorf("hash log writer is closed")
}

// The writer goroutine. Handles requests until a close or a failure. A failure leaves the mutable file unsealed;
// the next writer opened on the directory seals it.
func (w *HashLogWriter) run() {
	defer close(w.stopped)
	for request := range w.requests {
		done, err := w.handleRequest(request)
		if err != nil {
			logger.Error("hash log writer encountered an unrecoverable error; shutting down", "error", err)
			w.failure = err
			return
		}
		if done {
			return
		}
	}
}

// Handles one request, reporting whether it was a close. An error stops the writer.
func (w *HashLogWriter) handleRequest(request any) (done bool, err error) {
	switch r := request.(type) {
	case writeBlockRequest:
		if err := w.handleWriteBlock(r); err != nil {
			return false, fmt.Errorf("failed to write block %d: %w", r.blockNumber, err)
		}
		return false, nil
	case flushRequest:
		err := w.flush()
		r.reply <- err
		if err != nil {
			return false, fmt.Errorf("failed to flush: %w", err)
		}
		return false, nil
	case closeRequest:
		if err := w.sealMutableFile(); err != nil {
			return true, fmt.Errorf("failed to seal the mutable file on close: %w", err)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown hash log writer request %T", request)
	}
}

// Appends the request's block to a mutable file whose header is the block's columns, opening one if none is open
// or the open one's header differs, and sealing the file once it reaches its target size.
func (w *HashLogWriter) handleWriteBlock(request writeBlockRequest) error {
	columns := rowColumns(request.blockNumber, request.hashes)
	if w.mutableFile != nil && !slices.Equal(columns, w.mutableFile.HashTypes()) {
		if err := w.sealMutableFile(); err != nil {
			return fmt.Errorf("failed to seal the file of the previous columns: %w", err)
		}
	}
	if w.mutableFile == nil {
		if err := w.openMutableFile(columns); err != nil {
			return fmt.Errorf("failed to open a file for the block's columns: %w", err)
		}
	}
	// An illegal name is not a column, and a row only writes its file's columns, so it is dropped here.
	if err := w.mutableFile.Write(&HashLog{BlockNumber: request.blockNumber, Hashes: request.hashes}); err != nil {
		return fmt.Errorf("failed to append the row: %w", err)
	}
	w.latestBlock = max(w.latestBlock, request.blockNumber)
	if w.mutableFile.Size() >= uint64(w.config.TargetFileSize) {
		if err := w.sealMutableFile(); err != nil {
			return fmt.Errorf("failed to seal a full file: %w", err)
		}
	}
	return nil
}

// Returns the legal names in hashes, sorted, which are the columns of the block's row. Logs each illegal name.
func rowColumns(blockNumber uint64, hashes map[string][]byte) []string {
	columns := make([]string, 0, len(hashes))
	for name := range hashes {
		if !legalHashTypeRegex.MatchString(name) {
			logger.Error("dropping a hash whose name contains illegal characters",
				"blockNumber", blockNumber, "name", name, "mustMatch", legalHashTypeRegex.String())
			continue
		}
		columns = append(columns, name)
	}
	sort.Strings(columns)
	return columns
}

// Opens a mutable file at the next file index, with columns as its header.
func (w *HashLogWriter) openMutableFile(columns []string) error {
	file, err := newHashLogFile(w.config.Path, w.nextFileIndex, w.config.Version, columns)
	if err != nil {
		return fmt.Errorf("failed to create hash log file %d: %w", w.nextFileIndex, err)
	}
	w.mutableFile = file
	w.nextFileIndex++
	return nil
}

// Seals the mutable file, records it as sealed, and runs GC. Does nothing if no file is open.
func (w *HashLogWriter) sealMutableFile() error {
	if w.mutableFile == nil {
		return nil
	}
	sealed := w.mutableFile.SealedInfo()
	if err := w.mutableFile.Close(); err != nil {
		return fmt.Errorf("failed to seal hash log file %s: %w", sealed.name, err)
	}
	w.mutableFile = nil
	w.sealedFiles[sealed.index] = &sealed
	w.currentDiskSpaceUsed += sealed.size
	w.runGC()
	return nil
}

// Flushes and fsyncs the mutable file, if one is open, and fsyncs the directory.
func (w *HashLogWriter) flush() error {
	if w.mutableFile != nil {
		if err := w.mutableFile.Flush(); err != nil {
			return fmt.Errorf("failed to flush the mutable file: %w", err)
		}
	}
	if err := util.SyncPath(w.config.Path); err != nil {
		return fmt.Errorf("failed to sync hash log directory %s: %w", w.config.Path, err)
	}
	return nil
}

// Seals the unsealed files a crash left behind, then records every sealed file in the directory, so that file
// indices and GC continue from the previous session.
func (w *HashLogWriter) scanDirectory() error {
	entries, err := os.ReadDir(w.config.Path)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", w.config.Path, err)
	}
	for _, entry := range entries {
		isHashLog, isSealed := isHashLogFileName(entry.Name())
		if isHashLog && !isSealed {
			if err := sealHashLog(filepath.Join(w.config.Path, entry.Name())); err != nil {
				return fmt.Errorf("failed to seal orphaned file %s: %w", entry.Name(), err)
			}
		}
	}

	// Re-read the directory now that orphans have been sealed (or removed).
	entries, err = os.ReadDir(w.config.Path)
	if err != nil {
		return fmt.Errorf("failed to re-read directory %s: %w", w.config.Path, err)
	}
	hasSealedFiles := false
	for _, entry := range entries {
		parsed, ok := parseFileName(entry.Name())
		if !ok || !parsed.sealed {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", entry.Name(), err)
		}
		size := uint64(info.Size()) //nolint:gosec // file size is non-negative
		w.sealedFiles[parsed.index] = &sealedFileInfo{
			index:      parsed.index,
			name:       entry.Name(),
			firstBlock: parsed.firstBlock,
			lastBlock:  parsed.lastBlock,
			size:       size,
		}
		w.currentDiskSpaceUsed += size
		w.latestBlock = max(w.latestBlock, parsed.lastBlock)
		if !hasSealedFiles || parsed.index < w.lowestLogFileIndex {
			w.lowestLogFileIndex = parsed.index
		}
		w.nextFileIndex = max(w.nextFileIndex, parsed.index+1)
		hasSealedFiles = true
	}
	return nil
}

// Deletes the oldest sealed files while the block-count retention window or the disk-size cap is exceeded. Runs
// only while no mutable file is open.
func (w *HashLogWriter) runGC() {
	for w.lowestLogFileIndex < w.nextFileIndex {
		idx := w.lowestLogFileIndex
		info, ok := w.sealedFiles[idx]
		if !ok {
			// A gap left by a file that held no blocks and was removed.
			w.lowestLogFileIndex++
			continue
		}

		// A file is over the window once its newest block is blocksToRetain or more behind the latest. Written
		// as an addition to avoid unsigned underflow.
		blocksToRetain := uint64(w.config.BlocksToRetain)
		maxDiskSize := uint64(w.config.MaxDiskSize)
		overBlockRetention := blocksToRetain > 0 && info.lastBlock+blocksToRetain <= w.latestBlock
		overSizeCap := maxDiskSize > 0 && w.currentDiskSpaceUsed > maxDiskSize
		if !overBlockRetention && !overSizeCap {
			return
		}

		if err := w.deleteSealedFile(info); err != nil {
			logger.Error("failed to delete sealed hash log file during GC", "index", idx, "error", err)
			return
		}
		w.lowestLogFileIndex++
	}
}

// Deletes a sealed file and removes it from the bookkeeping.
func (w *HashLogWriter) deleteSealedFile(info *sealedFileInfo) error {
	path := filepath.Join(w.config.Path, info.name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove sealed file %s: %w", path, err)
	}
	w.currentDiskSpaceUsed -= info.size
	delete(w.sealedFiles, info.index)
	return nil
}
