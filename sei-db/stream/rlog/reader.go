package rlog

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/tidwall/wal"
)

var (
	_ Reader = (*RLReader)(nil)
)

type RLReader struct {
	rlog       *wal.Log
	logger     logger.Logger
	config     Config
	errSignal  chan error
	stopSignal chan struct{}
	offset     uint64
}

func NewReader(logger logger.Logger, rlog *wal.Log, config Config) (*RLReader, error) {
	return &RLReader{rlog: rlog, logger: logger, config: config}, nil
}

// Replay will read the replay log and process each log entry with the provided function
func (reader *RLReader) Replay(start uint64, end uint64, processFn func(index uint64, entry proto.ReplayLogEntry) error) error {
	for i := start; i <= end; i++ {
		var entry proto.ReplayLogEntry
		bz, err := reader.rlog.Read(i)
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

func (reader *RLReader) StartSubscriber(fromIndex uint64, processFn func(index uint64, entry proto.ReplayLogEntry) error) {
	if reader.stopSignal == nil {
		reader.errSignal = make(chan error)
		reader.stopSignal = make(chan struct{}, 1)
		go func() {
			reader.offset = fromIndex
			for {
				// Check if the subscriber is stopped
				select {
				case <-reader.stopSignal:
					break
				default:
				}

				// Check the last written index of the log
				lastIndex, err := reader.rlog.LastIndex()
				if err != nil {
					reader.errSignal <- err
					break
				}
				if reader.offset <= lastIndex {
					// if we are behind latest, read next entry and process it
					entry, err := reader.ReadAt(reader.offset)
					if err != nil {
						reader.errSignal <- err
						break
					}
					err = processFn(reader.offset, *entry)
					if err != nil {
						reader.errSignal <- err
						break
					}
					reader.offset++
				} else {
					// otherwise, we are caught up, sleep for a while
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()

	}
}

// ReadAt will read the log entry at the provided index
func (reader *RLReader) ReadAt(index uint64) (*proto.ReplayLogEntry, error) {
	var entry = &proto.ReplayLogEntry{}
	bz, err := reader.rlog.Read(index)
	if err != nil {
		return entry, fmt.Errorf("read rlog failed, %w", err)
	}
	if err := entry.Unmarshal(bz); err != nil {
		return entry, fmt.Errorf("unmarshal rlog failed, %w", err)
	}
	return entry, nil
}

// CheckSubscriber check the error signal of the subscriber
func (reader *RLReader) CheckSubscriber() error {
	if reader.errSignal == nil {
		return errors.New("subscriber is not started")
	}
	select {
	case err := <-reader.errSignal:
		// async wal writing failed, we need to abort the state machine
		return fmt.Errorf("reader subscribe goroutine quit unexpectedly: %w", err)
	default:
	}
	return nil
}

func (reader *RLReader) StopSubscriber() error {
	if reader.stopSignal != nil {
		select {
		case reader.stopSignal <- struct{}{}:
			close(reader.stopSignal)
			reader.stopSignal = nil
		default:
		}
	}
	return nil
}
