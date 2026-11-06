package datalake

import (
	"context"
	"fmt"
	"math"
	"time"
)

// run processes requests until ctx ends. Between requests, it collects finished writes and deletes, writes metadata
// and starts deletes. It also wakes for this when a write finishes and on each tick of
// config.MetadataWriteInterval.
func (s *cdkDataLake) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.config.MetadataWriteInterval)
	defer ticker.Stop()

	for {
		s.collectFinishedOperations()
		s.startMetadataWriteIfDue()
		s.submitDeletes()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.writer.Finished():
		case request := <-s.requests:
			s.handleRequest(ctx, request)
		}
	}
}

// handleRequest handles request, which must be one of the request types the requests channel carries.
func (s *cdkDataLake) handleRequest(
	// Ends when the loop stops.
	ctx context.Context,
	request any,
) {
	switch request := request.(type) {
	case *putRequest:
		s.handlePut(request)
	case *deleteRequest:
		request.reply <- s.acceptDelete(request.key)
	case *pruneRequest:
		request.reply <- s.handlePrune(request.key)
	case *flushRequest:
		s.handleFlush(ctx, request)
	case *lookupRequest:
		request.reply <- lookupResult{present: s.contains(request.key), err: s.failure}
	case *boundsRequest:
		request.reply <- s.currentBounds()
	case *iteratorRequest:
		request.reply <- s.currentKeyTopology()
	default:
		panic(fmt.Sprintf("control loop received a request of unknown type %T", request))
	}
}

// isEmpty reports whether the store holds no keys.
func (s *cdkDataLake) isEmpty() bool {
	if s.allowGaps {
		return s.keys.Len() == 0
	}
	return s.nextKey == s.deleter.ScheduledDeletionWatermark()
}

// contains reports whether key has been accepted and not deleted.
func (s *cdkDataLake) contains(key uint64) bool {
	if s.isEmpty() || key < s.deleter.ScheduledDeletionWatermark() || key >= s.nextKey {
		return false
	}
	if s.allowGaps {
		_, found := s.keys.Get(key)
		return found
	}
	return true
}

// firstKey returns the lowest key in the store, and false if the store is empty.
func (s *cdkDataLake) firstKey() (uint64, bool) {
	if s.isEmpty() {
		return 0, false
	}
	if s.allowGaps {
		first, _, _ := s.keys.PeekFront()
		return first, true
	}
	return s.deleter.ScheduledDeletionWatermark(), true
}

// durableWatermark returns the watermark strictly below which every accepted put is complete.
func (s *cdkDataLake) durableWatermark() uint64 {
	if operation, ok := s.putOperations.TryPeekFront(); ok {
		return operation.Key()
	}
	return s.nextKey
}

// currentBounds answers a boundsRequest.
func (s *cdkDataLake) currentBounds() storeBounds {
	first, ok := s.firstKey()
	return storeBounds{watermark: s.durableWatermark(), firstKey: first, firstKeyOK: ok}
}

// currentKeyTopology answers an iteratorRequest.
func (s *cdkDataLake) currentKeyTopology() keyTopologyResult {
	if s.failure != nil {
		return keyTopologyResult{err: s.failure}
	}
	snapshot, err := s.metadataManager.Snapshot(s.durableWatermark())
	if err != nil {
		s.failure = err
		return keyTopologyResult{err: err}
	}
	topology := snapshot.KeyTopology()
	if topology.AllowGaps() {
		watermark := snapshot.DurableWatermark()
		keys := make([]uint64, 0, s.keys.Len())
		for key := range s.keys.Forward() {
			if key >= watermark {
				break
			}
			keys = append(keys, key)
		}
		topology = topology.WithKeys(keys)
	}
	return keyTopologyResult{topology: topology}
}

// handlePut accepts or rejects a put, then records an accepted put and hands it to the writer, waiting while the
// writer's queue is full.
func (s *cdkDataLake) handlePut(request *putRequest) {
	err := s.acceptPut(request.key)
	request.reply <- err
	if err != nil {
		if request.stream != nil {
			_ = request.stream.Close()
		}
		return
	}

	s.nextKey = request.key + 1
	if s.allowGaps {
		s.keys.Put(request.key, struct{}{})
	}
	if request.stream != nil {
		s.putOperations.PushBack(s.writer.PutStream(request.key, request.stream))
	} else {
		s.putOperations.PushBack(s.writer.Put(request.key, request.value))
	}
}

// acceptPut validates a put of key. The first put into an empty store also writes the metadata that records where the
// store starts, and waits until it is durable, so that no data object can exist without metadata covering it.
func (s *cdkDataLake) acceptPut(key uint64) error {
	if s.failure != nil {
		return fmt.Errorf("data lake has failed: %w", s.failure)
	}
	if key == math.MaxUint64 {
		return fmt.Errorf("key %d is not supported", key)
	}
	empty := s.isEmpty()
	if key < s.nextKey {
		return fmt.Errorf("key %d is not above the store's keys and deleted keys, the next must be at least %d",
			key, s.nextKey)
	}
	if !s.allowGaps && !empty && key != s.nextKey {
		return fmt.Errorf("key %d would leave a gap, the next key must be %d", key, s.nextKey)
	}

	if empty {
		s.deleter.Start(key)
		s.nextKey = key
		if err := s.metadataManager.WriteAndWait(s.durableWatermark()); err != nil {
			s.failure = err
			return fmt.Errorf("record the store's first key: %w", err)
		}
	}
	return nil
}

// acceptDelete validates a delete and hands it to the deleter. A store without gaps may delete only its lowest key.
func (s *cdkDataLake) acceptDelete(key uint64) error {
	if s.failure != nil {
		return fmt.Errorf("data lake has failed: %w", s.failure)
	}
	if !s.contains(key) {
		return nil
	}
	if key == s.nextKey-1 {
		return fmt.Errorf("key %d is the highest key and cannot be deleted", key)
	}
	if first, _ := s.firstKey(); key != first && !s.allowGaps {
		return fmt.Errorf("key %d is not the lowest key, the only key a store without gaps may delete", key)
	}
	s.deleter.Delete(key)
	return nil
}

// handlePrune validates a prune below bound and hands it to the deleter.
func (s *cdkDataLake) handlePrune(bound uint64) error {
	if s.failure != nil {
		return fmt.Errorf("data lake has failed: %w", s.failure)
	}
	if bound <= s.deleter.ScheduledDeletionWatermark() {
		return nil
	}
	if s.isEmpty() || bound > s.nextKey-1 {
		return fmt.Errorf("pruning below %d would remove the highest key", bound)
	}
	s.deleter.Prune(bound)
	return nil
}

// handleFlush completes a Flush() call before the loop takes another request. It waits until no put or delete is in
// flight and the store's metadata is durable. A store that has never been written has nothing to flush.
func (s *cdkDataLake) handleFlush(
	// Ends when the loop stops.
	ctx context.Context,
	request *flushRequest,
) {
	if !s.metadataManager.Written() {
		request.reply <- s.failure
		return
	}
	for {
		s.collectFinishedOperations()
		if s.failure != nil {
			request.reply <- s.failure
			return
		}
		current, err := s.metadataManager.WriteIfChanged(s.durableWatermark())
		if err != nil {
			s.failure = err
			request.reply <- err
			return
		}
		if current && s.putOperations.IsEmpty() && s.deleter.Idle() {
			request.reply <- nil
			return
		}
		s.submitDeletes()

		select {
		case <-ctx.Done():
			return
		case <-s.writer.Finished():
		}
	}
}

// submitDeletes hands the writer up to config.DeletesPerCycle deletes that may start now, waiting while the writer's
// queue is full.
func (s *cdkDataLake) submitDeletes() {
	if s.failure != nil {
		return
	}
	for range s.config.DeletesPerCycle {
		write, ok := s.deleter.Next(s.durableWatermark())
		if !ok {
			return
		}
		s.writer.SubmitDelete(write)
	}
}

// collectFinishedOperations applies the outcomes of finished operations. Each queue is collected in order, stopping
// at its first operation still in flight. A failed operation fails the store.
func (s *cdkDataLake) collectFinishedOperations() {
	if s.failure != nil {
		return
	}
	if err := s.metadataManager.Collect(); err != nil {
		s.failure = err
		return
	}
	if err := s.collectPuts(); err != nil {
		s.failure = err
		return
	}
	if err := s.deleter.Collect(); err != nil {
		s.failure = fmt.Errorf("background delete failed: %w", err)
	}
}

// collectPuts advances the durable watermark past every leading finished put.
func (s *cdkDataLake) collectPuts() error {
	for !s.putOperations.IsEmpty() {
		err, done := s.putOperations.PeekFront().Check()
		if !done {
			return nil
		}
		if err != nil {
			return fmt.Errorf("background write failed: %w", err)
		}
		s.putOperations.PopFront()
	}
	return nil
}

// startMetadataWriteIfDue has the metadata manager start a metadata write if one is due.
func (s *cdkDataLake) startMetadataWriteIfDue() {
	if s.failure != nil {
		return
	}
	if err := s.metadataManager.WriteIfDue(s.durableWatermark()); err != nil {
		s.failure = err
	}
}
