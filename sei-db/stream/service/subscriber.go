package service

import (
	"errors"
	"fmt"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/stream"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"time"
)

type Subscriber struct {
	logger        logger.Logger
	logStream     stream.Stream[proto.ChangelogEntry]
	processFn     func(index uint64, entry proto.ChangelogEntry) error
	readErrSignal chan error
	stopSignal    chan struct{}
	currOffset    uint64
}

func NewSubscriber(
	logger logger.Logger,
	dir string,
	processFn func(index uint64, entry proto.ChangelogEntry) error,
) *Subscriber {
	logStream, err := changelog.NewStream(logger, dir, changelog.Config{ZeroCopy: true})
	if err != nil {
		panic(err)
	}
	return &Subscriber{
		logger:    logger,
		logStream: logStream,
		processFn: processFn,
	}
}

func (subscriber *Subscriber) Initialize(initialVersion uint32, lastVersion int64) error {
	startOffset := utils.VersionToIndex(lastVersion, initialVersion)
	return subscriber.CatchupToLatest(startOffset)
}

// Start starts the underline subscriber goroutine to keep read the replay log from a given index
func (subscriber *Subscriber) Start(startOffset uint64) {
	if subscriber.stopSignal == nil {
		subscriber.readErrSignal = make(chan error)
		subscriber.stopSignal = make(chan struct{}, 1)
		go func() {
			subscriber.currOffset = startOffset
			for {
				// Check if the subscriber is stopped
				select {
				case <-subscriber.stopSignal:
					break
				default:
				}

				// Check the last written index of the log
				lastIndex, err := subscriber.logStream.LastOffset()
				if err != nil {
					subscriber.readErrSignal <- err
					break
				}
				if subscriber.currOffset <= lastIndex {
					// if we are behind latest, read next entry and process it
					entry, err := subscriber.logStream.ReadAt(subscriber.currOffset)
					if err != nil {
						subscriber.readErrSignal <- err
						break
					}
					err = subscriber.processFn(subscriber.currOffset, *entry)
					if err != nil {
						subscriber.readErrSignal <- err
						break
					}
					subscriber.currOffset++
				} else {
					// otherwise, we are caught up, sleep for a while
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}
}

func (subscriber *Subscriber) CatchupToLatest(fromIndex uint64) error {
	latestOffset, err := subscriber.logStream.LastOffset()
	if err != nil {
		return err
	}
	for fromIndex <= latestOffset {
		entry, err := subscriber.logStream.ReadAt(fromIndex)
		if err != nil {
			return err
		}
		err = subscriber.processFn(fromIndex, *entry)
		if err != nil {
			return err
		}
		fromIndex++
	}
	return nil
}

func (subscriber *Subscriber) GetLatestOffset() (uint64, error) {
	return subscriber.logStream.LastOffset()
}

// CheckError check the error signal of the subscriber
func (subscriber *Subscriber) CheckError() error {
	if subscriber.readErrSignal == nil {
		return errors.New("subscriber is not started")
	}
	select {
	case err := <-subscriber.readErrSignal:
		// we need to abort the state machine
		return fmt.Errorf("reader subscribe goroutine quit unexpectedly: %w", err)
	default:
	}
	return nil
}

func (subscriber *Subscriber) Stop() error {
	if subscriber.stopSignal != nil {
		select {
		case subscriber.stopSignal <- struct{}{}:
			close(subscriber.stopSignal)
			subscriber.stopSignal = nil
		default:
		}
	}
	return nil
}
