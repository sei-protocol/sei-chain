package cdkdatalake

import (
	"context"
	"fmt"
	"math"
	"time"
)

// run processes requests until ctx ends or the store fails, recording the failure. Between requests, it finalizes
// finished writes and deletes, writes metadata and starts deletes. It also wakes for this when a write finishes and
// on each tick of config.MetadataWriteInterval.
func (s *cdkDataLake) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.config.MetadataWriteInterval)
	defer ticker.Stop()

	for {
		if err := s.finalizeOperations(); err != nil {
			s.failure = fmt.Errorf("finalize operations: %w", err)
			return
		}
		err := s.metadataManager.Write(s.scheduledDeletionWatermark, s.durableWatermark(), false, false)
		if err != nil {
			s.failure = fmt.Errorf("write metadata: %w", err)
			return
		}
		s.deleter.DoBackgroundWork(s.durableWatermark(), s.config.DeletesPerCycle)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.writer.Finished():
		case request := <-s.requests:
			if err := s.handleRequest(request); err != nil {
				s.failure = fmt.Errorf("handle request: %w", err)
				return
			}
		}
	}
}

// handleRequest handles request, which must be one of the request types the requests channel carries. A request
// that fails the store gets no reply.
func (s *cdkDataLake) handleRequest(request any) error {
	switch request := request.(type) {
	case *putRequest:
		return s.handlePut(request)
	case *deleteRequest:
		request.reply <- s.acceptDelete(request.key)
	case *pruneRequest:
		request.reply <- s.handlePrune(request.key)
	case *flushRequest:
		return s.handleFlush(request)
	case *lookupRequest:
		request.reply <- s.contains(request.key)
	case *boundsRequest:
		request.reply <- s.currentBounds()
	case *iteratorRequest:
		topology, err := s.currentKeyTopology()
		if err != nil {
			return fmt.Errorf("build key topology: %w", err)
		}
		request.reply <- topology
	default:
		panic(fmt.Sprintf("control loop received a request of unknown type %T", request))
	}
	return nil
}

// isEmpty reports whether the store holds no keys.
func (s *cdkDataLake) isEmpty() bool {
	_, ok := s.firstKey()
	return !ok
}

// contains reports whether key has been accepted and not deleted.
func (s *cdkDataLake) contains(key uint64) bool {
	if key < s.scheduledDeletionWatermark || key >= s.nextKey {
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
	if !s.allowGaps {
		return s.scheduledDeletionWatermark, s.scheduledDeletionWatermark < s.nextKey
	}
	for key := range s.keys.Forward() {
		if key >= s.scheduledDeletionWatermark {
			return key, true
		}
	}
	return 0, false
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
	deletionWatermark, found := s.deleter.LowestRemainingKey()
	if !found {
		deletionWatermark = s.nextKey
	}
	return storeBounds{
		watermark:         s.durableWatermark(),
		deletionWatermark: deletionWatermark,
		firstKey:          first,
		firstKeyOK:        ok,
	}
}

// currentKeyTopology answers an iteratorRequest.
func (s *cdkDataLake) currentKeyTopology() (keyTopology, error) {
	metadata, err := s.metadataManager.CurrentMetadata(s.scheduledDeletionWatermark, s.durableWatermark())
	if err != nil {
		return keyTopology{}, fmt.Errorf("build current metadata: %w", err)
	}
	topology := metadata.KeyTopology()
	if topology.AllowGaps() {
		watermark := metadata.DurableWatermark()
		keys := make([]uint64, 0, s.keys.Len())
		for key := range s.keys.Forward() {
			if key >= watermark {
				break
			}
			if key >= s.scheduledDeletionWatermark {
				keys = append(keys, key)
			}
		}
		topology = topology.WithKeys(keys)
	}
	return topology, nil
}

// handlePut accepts or rejects a put, then records an accepted put and hands it to the writer, waiting while the
// writer's queue is full. The first put into an empty store also writes the metadata that records where the store
// starts, and waits until it is durable, so that no data object can exist without metadata covering it.
func (s *cdkDataLake) handlePut(request *putRequest) error {
	if err := s.validatePut(request.key); err != nil {
		request.reply <- err
		if request.stream != nil {
			_ = request.stream.Close()
		}
		return nil
	}
	if s.isEmpty() {
		s.scheduledDeletionWatermark = request.key
		s.deleter.SetFirstKey(request.key)
		s.nextKey = request.key
		err := s.metadataManager.Write(s.scheduledDeletionWatermark, s.durableWatermark(), true, true)
		if err != nil {
			return fmt.Errorf("record the store's first key: %w", err)
		}
	}
	request.reply <- nil

	s.nextKey = request.key + 1
	if s.allowGaps {
		s.keys.Put(request.key, struct{}{})
	}
	if request.stream != nil {
		s.putOperations.PushBack(s.writer.PutStream(request.key, request.stream))
	} else {
		s.putOperations.PushBack(s.writer.Put(request.key, request.value))
	}
	return nil
}

// validatePut returns the reason a put of key is rejected, or nil if it is accepted.
func (s *cdkDataLake) validatePut(key uint64) error {
	if key == math.MaxUint64 {
		return fmt.Errorf("key %d is not supported", key)
	}
	if key < s.nextKey {
		return fmt.Errorf("key %d is not above the store's keys and deleted keys, the next must be at least %d",
			key, s.nextKey)
	}
	if !s.allowGaps && !s.isEmpty() && key != s.nextKey {
		return fmt.Errorf("key %d would leave a gap, the next key must be %d", key, s.nextKey)
	}
	return nil
}

// acceptDelete validates a delete, then prunes the lowest key or hands any other key to the deleter.
func (s *cdkDataLake) acceptDelete(key uint64) error {
	if !s.contains(key) {
		return nil
	}
	if key == s.nextKey-1 {
		return fmt.Errorf("key %d is the highest key and cannot be deleted", key)
	}
	if first, _ := s.firstKey(); key == first {
		s.scheduledDeletionWatermark = key + 1
		return nil
	}
	if err := s.deleter.Delete(key); err != nil {
		return fmt.Errorf("schedule the deletion: %w", err)
	}
	return nil
}

// handlePrune validates a prune below bound and raises the scheduled deletion watermark to bound.
func (s *cdkDataLake) handlePrune(bound uint64) error {
	if bound <= s.scheduledDeletionWatermark {
		return nil
	}
	if s.isEmpty() || bound > s.nextKey-1 {
		return fmt.Errorf("pruning below %d would remove the highest key", bound)
	}
	s.scheduledDeletionWatermark = bound
	return nil
}

// handleFlush completes a Flush() call before the loop takes another request. It waits for every write to finish,
// makes the metadata durable, performs every pending delete, and makes the metadata durable again. A store that has
// never been written has nothing to flush.
func (s *cdkDataLake) handleFlush(request *flushRequest) error {
	if durableMetadata := s.metadataManager.DurableMetadata(); durableMetadata.IsNewStore() {
		request.reply <- struct{}{}
		return nil
	}

	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("finish writes: %w", err)
	}
	if err := s.finalizeOperations(); err != nil {
		return fmt.Errorf("finalize writes: %w", err)
	}

	if err := s.metadataManager.Write(s.scheduledDeletionWatermark, s.durableWatermark(), true, true); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	// With the metadata durable, finalizing hands the deleter every prune.
	if err := s.finalizeOperations(); err != nil {
		return fmt.Errorf("finalize metadata write: %w", err)
	}

	s.deleter.DoBackgroundWork(s.durableWatermark(), math.MaxInt)
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("finish deletes: %w", err)
	}
	if err := s.finalizeOperations(); err != nil {
		return fmt.Errorf("finalize deletes: %w", err)
	}

	if err := s.metadataManager.Write(s.scheduledDeletionWatermark, s.durableWatermark(), true, true); err != nil {
		return fmt.Errorf("write final metadata: %w", err)
	}
	request.reply <- struct{}{}
	return nil
}

// finalizeOperations applies the outcomes of finished operations, and hands the deleter every key below the durable
// metadata's scheduled deletion watermark. Each queue is finalized in order, stopping at its first operation still in
// flight.
func (s *cdkDataLake) finalizeOperations() error {
	if err := s.metadataManager.FinalizeWrite(); err != nil {
		return fmt.Errorf("finalize metadata write: %w", err)
	}
	// A store without gaps may delete a key's object only once durable metadata records its deletion, since
	// recovery assumes every key from the scheduled deletion watermark up to the durable watermark exists.
	durableMetadata := s.metadataManager.DurableMetadata()
	s.deleter.Prune(durableMetadata.ScheduledDeletionWatermark())
	for !s.putOperations.IsEmpty() {
		err, done := s.putOperations.PeekFront().Check()
		if !done {
			break
		}
		if err != nil {
			return fmt.Errorf("background write failed: %w", err)
		}
		s.putOperations.PopFront()
	}
	if err := s.deleter.FinalizeDeletes(); err != nil {
		return fmt.Errorf("background delete failed: %w", err)
	}
	return nil
}
