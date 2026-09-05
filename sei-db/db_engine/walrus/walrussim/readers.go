package walrussim

import (
	"bytes"
	"fmt"
	"time"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus"
)

// The key class labels reads are separated by, describing which pool the reader sampled from.
const keyClassLive = "live"

// The label for reads against ids no class writes.
const keyClassNeverWritten = "never_written"

// The presence label for a read where nothing exists to find: no entry for the key at or below the queried
// block. These walk every pod and then the floor snapshot, so they are the engine's worst case.
const presenceMissing = "missing"

// The presence label for a read where the key exists somewhere at or below the queried block, whether as a
// value or a tombstone. These stop as soon as the walk reaches it.
const presencePresent = "present"

// startReaders launches the goroutines that issue historical reads.
func (s *WalrusSim) startReaders() {
	if s.config.ReadConcurrency == 0 {
		return
	}

	perReader := s.config.ReadsPerSecond / s.config.ReadConcurrency
	if perReader < 1 {
		perReader = 1
	}

	for index := 0; index < s.config.ReadConcurrency; index++ {
		// CannedRandom is not safe to share once its cursor is in use, so each reader takes its own clone at
		// a different offset.
		random := s.workload.random.Clone(true)
		s.readers.Add(1)
		go s.readLoop(perReader, random)
	}

	fmt.Printf("Started %d reader goroutines (%d reads/sec each)\n", s.config.ReadConcurrency, perReader)
}

// readLoop issues reads at a fixed rate until the run stops.
func (s *WalrusSim) readLoop(readsPerSecond int, random *crand.CannedRandom) {
	defer s.readers.Done()

	ticker := time.NewTicker(time.Second / time.Duration(readsPerSecond))
	defer ticker.Stop()

	for {
		select {
		case <-s.stopReaders:
			return
		case <-s.context.Done():
			return
		case <-ticker.C:
			s.executeRead(random)
		}
	}
}

// executeRead issues one historical read and checks the answer.
//
// Everything the read needs — the key bytes and what the workload says the answer should be — is computed
// before the timer starts, and the comparison happens after it stops, so neither lands in the measurement.
func (s *WalrusSim) executeRead(random *crand.CannedRandom) {
	ok, first, last, err := s.engine.QueryableBounds()
	if err != nil || !ok {
		return
	}

	//nolint:gosec // G115 - block numbers stay far below the int64 ceiling
	blockNumber := uint64(random.Int64Range(int64(first), int64(last)+1))
	id, keyClass := s.pickKey(random)
	key := s.workload.key(id)
	wantValue, wantFound, wantPresent := s.workload.expected(id, blockNumber)
	presence := presencePresent
	if !wantPresent {
		presence = presenceMissing
	}

	start := time.Now()
	value, status, err := s.engine.Get(key, blockNumber)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Printf("\nread of key %d at block %d failed: %v\n", id, blockNumber, err)
		return
	}
	s.reads.Add(1)
	recordRead(s.config.Name, presence, keyClass, status.String(), elapsed)

	if !s.config.VerifyReads {
		return
	}
	// A block can fall out of retention between choosing it and asking for it, and the newest blocks are
	// still being accumulated. Neither is a wrong answer.
	if status == walrus.ReadTooOld || status == walrus.ReadTooNew {
		return
	}
	s.verify(id, blockNumber, keyClass, status, value, wantValue, wantFound)
}

// pickKey chooses an id to read, splitting between the classes that write and the ids nothing writes.
func (s *WalrusSim) pickKey(random *crand.CannedRandom) (id uint64, keyClass string) {
	neverWritten := s.workload.neverWrittenLimit - s.workload.liveIDLimit
	if neverWritten > 0 && random.Float64() < s.config.NeverWrittenReadFraction {
		//nolint:gosec // G115 - id counts stay far below the int64 ceiling
		offset := uint64(random.Int64Range(0, int64(neverWritten)))
		return s.workload.liveIDLimit + offset, keyClassNeverWritten
	}
	//nolint:gosec // G115 - as above
	return uint64(random.Int64Range(0, int64(s.workload.liveIDLimit))), keyClassLive
}

// verify compares one answer against the workload's model and reports a disagreement.
func (s *WalrusSim) verify(
	id uint64,
	blockNumber uint64,
	keyClass string,
	status walrus.ReadStatus,
	value []byte,
	wantValue []byte,
	wantFound bool,
) {
	gotFound := status == walrus.ReadFound
	if gotFound == wantFound && (!wantFound || bytes.Equal(value, wantValue)) {
		return
	}

	s.mismatches.Add(1)
	recordMismatch(s.config.Name, keyClass)
	fmt.Printf("\nMISMATCH key %d at block %d: engine said %s with %d bytes, workload expected found=%v\n",
		id, blockNumber, status, len(value), wantFound)
}
