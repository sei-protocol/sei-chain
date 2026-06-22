package hashlog

import (
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ HashLogger = (*hashLoggerImpl)(nil)

// A standard hash logger implementation.
type hashLoggerImpl struct {

	// For sending work to the controller chan.
	controlChan chan any // TODO use more specific type

	// For sending work to the writer thread.
	writerChan chan any // TODO use more specific type

	// For sending work to the background worker thread.
	hashChan chan any // TODO use more specific type

	// The number of bytes currently used by log files.
	currentDiskSpaceUsed uint

	// The size of each hashlog file currently tracked (excluding the mutable file)
	fileSizes map[uint64]uint

	// the index of the oldest hash log file currently tracked
	lowestLogFileIndex uint64

	// the index of the mutable hash log file (i.e. the latest one)
	mutableLogFileIndex uint64
}

func newHashLoggerImpl(config *HashLoggerConfig) (*hashLoggerImpl, error) {
	// TODO validate config

	return &hashLoggerImpl{
		writerChan: make(chan any), // TODO use more specific type
		hashChan:   make(chan any), // TODO use more specific type
	}, nil
}

func (h *hashLoggerImpl) Close() error {
	panic("unimplemented")
}

func (h *hashLoggerImpl) ReportDiff(blockNumber uint64, cs []*proto.NamedChangeSet) {
	// send the diff to the hasher thread
	// if the hasher's channel is full, drop the diff and send a notification to the control loop
	panic("unimplemented")
}

func (h *hashLoggerImpl) ReportFlatKVHash(blockNumber uint64, hash []byte) error {
	// send this hash to the control loop
	panic("unimplemented")
}

func (h *hashLoggerImpl) ReportMemIAVLHash(blockNumber uint64, hash []byte) error {
	// send this hash to the control loop
	panic("unimplemented")
}

func (h *hashLoggerImpl) ReportRootHash(blockNumber uint64, hash []byte) error {
	// send this hash to the control loop
	panic("unimplemented")
}

func (h *hashLoggerImpl) controlLoop() {
	// gather messages from other threads, when there is a comiplete HashLog, send it to the writer thread
	// we want to send blocks in order, so if we are missing data for block N, delay sending data for blocks > N
	// if we've got a number of blocks waiting to be sent that exceeds MaxBlockDelay, send the oldest block even if it's not complete
	// if the writer thread's channel is full, drop the log
	// every time we seal a new hash log file, check to see if we need to do GC or not
	// if the block number ever goes backwards (i.e. some sort of rollback), we should close out the current file and start a new one
	for {
		// TODO implement this
	}
}

func (h *hashLoggerImpl) writer() {
	// pop the next HashLog to write then write it
	for {
		// TODO implement this
	}
}

func (h *hashLoggerImpl) hasher() {
	// pop the next hash, hash it, and send the result to the control loop
	for {
		// TODO implement this
	}
}
