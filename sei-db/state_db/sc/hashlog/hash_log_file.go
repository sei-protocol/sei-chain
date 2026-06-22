package hashlog

// Hashlog files use the following naming schema:
//
// For mutable files: {index}.hlog.u
// For sealed files: {index}-{first block}-{last block}.hlog

const (
	// The file extension for unsealed hash log files.
	HashLogUnsealedExtension = ".hlog.u"
	// The file extension for sealed hash log files.
	HashLogSealedExtension = ".hlog"
)

type hashLogFile struct {
	// The hash logs contained within this file
	logs []HashLog

	// If false, this file is mutable and can accept writes. If true, new writes are rejected.
	// Files are only unsealed if they were created fresh during the current session. Files on
	// disk at startup time are always considered sealed.
	sealed bool

	// The first block number that appears in this file, or 0 if this file is empty.
	firstBlockIndex uint64

	// The last block number that appears in this file, or 0 if this file is empty.
	lastBlockIndex uint64

	// Whether this file contains any blocks.
	hasBlocks bool

	// Each hash log file is given a unique index which monotonically increases with each new file.
	index int
}

// Check if a file name matches the expected pattern for hash log files, and if so, whether it's sealed or unsealed.
func isHashLogFileName(fileName string) (isHashLogFile bool, isSealed bool) {
	// TODO check if the file name matches the expected pattern for hash log files
	return false, false
}

// Parse the block numbers from a ".hlog" file.
func parseBlockNumbersFromFileName(fileName string) (firstBlockNumber uint64, lastBlockNumber uint64, err error) {
	return 0, 0, nil
}

// Create a new mutable hash log file.
func newHashLogFile(index int) *hashLogFile {
	return &hashLogFile{
		logs:   []HashLog{},
		sealed: false,
		index:  index,
	}
}

// Read a hash log file from disk.
func ReadHashLogFile(path string) (*hashLogFile, error) {
	// TODO
	// should be tolerant of truncated data, as it's possible some writes could have failed
	// we should always be able to detect if the write happened completely or not
	return nil, nil
}

// Write the next HashLog to this file. If the file is sealed, this returns an error.
func (h *hashLogFile) write(hashLog *HashLog) error {
	// TODO
	// stream to buffered writer
	return nil
}

// Close this file, flushing any buffered data to disk and marking it as sealed.
func (h *hashLogFile) close() error {
	// TODO flush buffered writer and mark this file as sealed
	// we should do an atomic rename
	return nil
}

// Used to seal a hash log file discovered on disk with the ".hlog.u" extension. This can happen if a node
// crashes before it manages to seal a hashlog file.
func sealHashLog(path string) error {
	// parse the hashlog file
	return nil
}
