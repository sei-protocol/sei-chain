package service

import (
	"time"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/stream"
	"github.com/sei-protocol/sei-db/stream/changelog"
)

type Subscriber struct {
	logger     logger.Logger
	logStream  stream.Stream[proto.ChangelogEntry]
	processFn  func(index uint64, entry proto.ChangelogEntry) error
	stopSignal chan struct{}
	currOffset uint64
}

// NewSubscriber creates a new subscriber service that will keep reading the log stream
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

// Start starts the underline subscriber goroutine to keep read the replay log from a given index
func (subscriber *Subscriber) Start(startOffset uint64) {
	if subscriber.stopSignal == nil {
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
					panic(err)
				}
				if subscriber.currOffset <= lastIndex {
					// if we are behind latest, read next entry and process it
					entry, err := subscriber.logStream.ReadAt(subscriber.currOffset)
					if err != nil {
						panic(err)
					}
					err = subscriber.processFn(subscriber.currOffset, *entry)
					if err != nil {
						panic(err)
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

// CatchupToLatest will replay the log and process each entry until the end of the log
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

// GetLatestOffset returns the end offset of the log
func (subscriber *Subscriber) GetLatestOffset() (uint64, error) {
	return subscriber.logStream.LastOffset()
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
