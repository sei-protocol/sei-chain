package wal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ GenericWALProcessor[proto.ChangelogEntry] = (*Processor)(nil)

type Processor struct {
	maxPendingSize   int
	chPendingEntries chan proto.ChangelogEntry
	errSignal        chan error
	stopSignal       chan struct{}
	processFn        func(entry proto.ChangelogEntry) error
}

func NewSubscriber(
	maxPendingSize int,
	processFn func(entry proto.ChangelogEntry) error,
) *Processor {
	subscriber := &Processor{
		maxPendingSize: maxPendingSize,
		processFn:      processFn,
	}

	return subscriber
}

func (s *Processor) Start() {
	if s.maxPendingSize > 0 {
		s.startAsyncProcessing()
	}
}

func (s *Processor) ProcessEntry(entry proto.ChangelogEntry) error {
	if s.maxPendingSize <= 0 {
		return s.processFn(entry)
	}
	s.chPendingEntries <- entry
	return s.CheckError()
}

func (s *Processor) startAsyncProcessing() {
	if s.chPendingEntries == nil {
		s.chPendingEntries = make(chan proto.ChangelogEntry, s.maxPendingSize)
		s.errSignal = make(chan error)
		s.stopSignal = make(chan struct{})
		go func() {
			defer close(s.errSignal)
			for {
				select {
				case entry := <-s.chPendingEntries:
					if err := s.processFn(entry); err != nil {
						s.errSignal <- err
					}
				case <-s.stopSignal:
					return
				}
			}
		}()
	}
}

func (s *Processor) Close() error {
	if s.chPendingEntries == nil {
		return nil
	}
	s.stopSignal <- struct{}{}
	close(s.chPendingEntries)
	err := s.CheckError()
	s.chPendingEntries = nil
	s.errSignal = nil
	s.stopSignal = nil
	return err
}

func (s *Processor) CheckError() error {
	select {
	case err := <-s.errSignal:
		// async wal writing failed, we need to abort the state machine
		return fmt.Errorf("subscriber failed unexpectedly: %w", err)
	default:
	}
	return nil
}
