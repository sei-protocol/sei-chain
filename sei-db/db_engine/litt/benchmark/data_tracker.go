package benchmark

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Layr-Labs/eigenda/litt/benchmark/config"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/docker/go-units"
)

// WriteInfo contains information needed to perform a write operation.
type WriteInfo struct {
	// The index of the key to write.
	KeyIndex uint64
	// The key to write.
	Key []byte
	// The value to write.
	Value []byte
}

// ReadInfo contains information needed to perform a read operation.
type ReadInfo struct {
	// The key to read.
	Key []byte
	// The value we expect to read.
	Value []byte
}

// DataTracker is responsible for tracking key-value pairs that have been written to the database, and for generating
// new key-value pairs to be written.
type DataTracker struct {
	ctx    context.Context
	cancel context.CancelFunc

	// A source of randomness.
	rand *rand.Rand

	// The configuration for the benchmark.
	config *config.BenchmarkConfig

	// The directory where cohort files are stored.
	cohortDirectory string

	// A map from cohort index to information about the cohort.
	cohorts map[uint64]*Cohort

	// The cohort that is currently being used to generate keys for writing.
	activeCohort *Cohort

	// A set of cohorts that have been completely written to the database (i.e. cohorts that are safe to read).
	completeCohortSet map[uint64]struct{}

	// A set of keys passed to ReportWrite() that have not yet been fully processed.
	writtenKeysSet map[uint64]struct{}

	// The index of the oldest cohort being tracked.
	lowestCohortIndex uint64

	// The index of the newest cohort being tracked.
	highestCohortIndex uint64

	// Consider all key indices that have been generated this session (i.e. ignore keys indices generated prior to the
	// most recent restart). We want to find the highest key index that has been written to the database AND
	// where all lower key indices have also been written as well.
	highestWrittenKeyIndex int64

	// Consider all cohorts that have been generated this session (i.e. ignore cohorts generated prior to the most
	// recent restart). We want to find the highest cohort index that has been fully written to the database AND
	// where all cohorts with lower indices have also been written as well.
	highestWrittenCohortIndex int64

	// A channel containing keys-value pairs that are ready to be written.
	writeInfoChan chan *WriteInfo

	// A channel containing keys that are ready to be read.
	readInfoChan chan *ReadInfo

	// A channel containing information about keys that have been written to the database.
	writtenKeyIndicesChan chan uint64

	// Responsible for producing "random" data for key-value pairs.
	generator *DataGenerator

	// The TTL minus a safety margin. Cohorts are considered to be expired if keys in them are older than this.
	safeTTL time.Duration

	// The size of the values in bytes for new cohorts.
	valueSize uint64

	// This channel has capacity one and initially has one value in it. This value is drained when the DataTracker is
	// fully stopped. Other threads can use this to block until the DataTracker is fully stopped.
	closedChan chan struct{}

	// Used to handle fatal errors in the DataTracker.
	errorMonitor *util.ErrorMonitor
}

// NewDataTracker creates a new DataTracker instance, loading all relevant cohorts from disk.
func NewDataTracker(
	ctx context.Context,
	config *config.BenchmarkConfig,
	errorMonitor *util.ErrorMonitor,
) (*DataTracker, error) {

	cohortDirectory := path.Join(config.MetadataDirectory, "cohorts")

	// Create the cohort directory if it doesn't exist.
	err := util.EnsureDirectoryExists(cohortDirectory, config.Fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to create cohort directory: %w", err)
	}

	lowestCohortIndex, highestCohortIndex, cohorts, err := gatherCohorts(cohortDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to gather cohorts: %w", err)
	}

	// Gather the set of complete cohorts. These are the cohorts we can read from.
	completeCohortSet := make(map[uint64]struct{})
	if len(cohorts) != 0 {
		for i := lowestCohortIndex; i <= highestCohortIndex; i++ {
			if cohorts[i].IsComplete() {
				completeCohortSet[i] = struct{}{}
			}
		}
	}

	valueSize := uint64(config.ValueSizeMB * float64(units.MiB))

	// Create an initial active cohort.
	var activeCohort *Cohort
	if len(cohorts) == 0 {
		// Starting fresh, create a new cohort starting from key index 0.
		activeCohort, err = NewCohort(
			cohortDirectory,
			0,
			0,
			config.CohortSize,
			valueSize,
			config.Fsync)
		if err != nil {
			return nil, fmt.Errorf("failed to create genesis cohort: %w", err)
		}
	} else {
		activeCohort, err = cohorts[highestCohortIndex].NextCohort(config.CohortSize, valueSize)
		if err != nil {
			return nil, fmt.Errorf("failed to create next cohort: %w", err)
		}
	}
	highestCohortIndex = activeCohort.CohortIndex()
	cohorts[highestCohortIndex] = activeCohort

	writeInfoChan := make(chan *WriteInfo, config.WriteInfoChanelSize)
	readInfoChan := make(chan *ReadInfo, config.ReadInfoChanelSize)
	writtenKeyIndicesChan := make(chan uint64, 64)

	ttl := time.Duration(config.TTLHours * float64(time.Hour))
	safetyMargin := time.Duration(config.ReadSafetyMarginMinutes * float64(time.Minute))
	safeTTL := ttl - safetyMargin

	closedChan := make(chan struct{}, 1)
	closedChan <- struct{}{} // Will be drained when the DataTracker is closed.

	ctx, cancel := context.WithCancel(ctx)

	tracker := &DataTracker{
		ctx:                       ctx,
		cancel:                    cancel,
		rand:                      rand.New(rand.NewSource(time.Now().UnixNano())),
		config:                    config,
		cohortDirectory:           cohortDirectory,
		cohorts:                   cohorts,
		completeCohortSet:         completeCohortSet,
		writtenKeysSet:            make(map[uint64]struct{}),
		writeInfoChan:             writeInfoChan,
		readInfoChan:              readInfoChan,
		writtenKeyIndicesChan:     writtenKeyIndicesChan,
		activeCohort:              activeCohort,
		lowestCohortIndex:         lowestCohortIndex,
		highestCohortIndex:        highestCohortIndex,
		highestWrittenKeyIndex:    int64(activeCohort.LowKeyIndex()) - 1,
		highestWrittenCohortIndex: int64(highestCohortIndex) - 1,
		safeTTL:                   safeTTL,
		valueSize:                 valueSize,
		generator:                 NewDataGenerator(config.Seed, config.RandomPoolSize),
		closedChan:                closedChan,
		errorMonitor:              errorMonitor,
	}

	go tracker.dataGenerator()

	return tracker, nil
}

// gatherCohorts loads cohorts from files on disk. The lowest/highest cohort indices are valid if and only if the
// cohorts map is not empty. If no cohorts are found, the lowest and highest cohort indices will be 0.
func gatherCohorts(cohortDirPath string) (
	lowestCohortIndex uint64,
	highestCohortIndex uint64,
	cohorts map[uint64]*Cohort,
	err error) {

	cohorts = make(map[uint64]*Cohort)

	// walk over files in path
	// for each file, check if it is a cohort file
	// if it is, load the cohort and add it to the map
	// if it is not, ignore it
	files, err := os.ReadDir(cohortDirPath)
	if err != nil {
		return 0,
			0,
			nil,
			fmt.Errorf("failed to read directory: %w", err)
	}

	lowestCohortIndex = math.MaxUint64
	highestCohortIndex = 0

	for _, file := range files {
		filePath := path.Join(cohortDirPath, file.Name())

		if strings.HasSuffix(filePath, CohortFileExtension) {
			cohort, err := LoadCohort(filePath)
			if err != nil {
				return 0,
					0,
					nil,
					fmt.Errorf("failed to load cohort: %w", err)
			}
			cohorts[cohort.CohortIndex()] = cohort

			if cohort.CohortIndex() < lowestCohortIndex {
				lowestCohortIndex = cohort.CohortIndex()
			}
			if cohort.cohortIndex > highestCohortIndex {
				highestCohortIndex = cohort.CohortIndex()
			}
		} else if strings.HasSuffix(filePath, CohortSwapFileExtension) {
			// Delete any swap files discovered
			err = os.Remove(filePath)
			if err != nil && !os.IsNotExist(err) {
				return 0,
					0,
					nil,
					fmt.Errorf("failed to delete swap file: %w", err)
			}
		}
	}

	if len(cohorts) == 0 {
		// Special case, no cohorts found.
		return 0, 0, cohorts, nil
	}

	return lowestCohortIndex, highestCohortIndex, cohorts, nil
}

// LargestReadableValueSize returns the size of the largest value possible to read from the database,
// given current configuration. Considers both values previously written and stored
// (possibly with different configurations), and values that may be written in the future with the
// current configuration.
func (t *DataTracker) LargestReadableValueSize() uint64 {
	largestValue := uint64(t.config.ValueSizeMB * float64(units.MiB))

	if len(t.cohorts) > 0 {
		for i := t.lowestCohortIndex; i <= t.highestCohortIndex; i++ {
			cohort := t.cohorts[i]
			if cohort.IsComplete() {
				if cohort.ValueSize() > largestValue {
					largestValue = cohort.ValueSize()
				}
			}
		}
	}

	return largestValue
}

// GetWriteInfo returns information required to perform a write operation. It returns the key index (which is needed to
// call MarkHighestIndexWritten()), the key, and the value. Data is generated on background goroutines in order to
// make this method very fast. Will not block as long as data can be generated in the background fast enough.
// May return nil if the context is cancelled.
func (t *DataTracker) GetWriteInfo() *WriteInfo {
	select {
	case info := <-t.writeInfoChan:
		return info
	case <-t.ctx.Done():
		return nil
	}
}

// ReportWrite is called when a key has been written to the database. This means that the key is now safe to be read.
func (t *DataTracker) ReportWrite(index uint64) {
	select {
	case t.writtenKeyIndicesChan <- index:
		return
	case <-t.ctx.Done():
		return
	}
}

// GetReadInfo returns information required to perform a read operation. Blocks until there is data eligible to be read.
func (t *DataTracker) GetReadInfo() *ReadInfo {
	select {
	case info := <-t.readInfoChan:
		return info
	case <-t.ctx.Done():
		return nil
	}
}

// GetReadInfoWithTimeout returns information required to perform a read operation. Waits the specified timeout for
// data to be eligible to be read. If no data is available within the time limit, returns nil.
func (t *DataTracker) GetReadInfoWithTimeout(timeout time.Duration) *ReadInfo {
	ctx, cancel := context.WithTimeout(t.ctx, timeout)
	defer cancel()

	select {
	case info := <-t.readInfoChan:
		return info
	case <-ctx.Done():
		return nil
	}
}

// Close stops the key manager's background tasks.
func (t *DataTracker) Close() {
	t.cancel()
	t.closedChan <- struct{}{}
	<-t.closedChan
}

// dataGenerator is responsible for generating data in the background.
func (t *DataTracker) dataGenerator() {
	ticker := time.NewTicker(time.Duration(t.config.CohortGCPeriodSeconds * float64(time.Second)))
	defer func() {
		ticker.Stop()
		<-t.closedChan
	}()

	nextWriteInfo := t.generateNextWriteInfo()
	nextReadInfo := t.generateNextReadInfo()

	for {
		if nextReadInfo == nil {
			// Edge case: when stared up for the first time, there won't be any values eligible to be read.
			// We have to handle this in a special manner to prevent nil values from being inserted into
			// the readInfoChan.

			select {
			case <-t.errorMonitor.ImmediateShutdownRequired():
				return
			case <-t.ctx.Done():
				return
			case keyIndex := <-t.writtenKeyIndicesChan:
				// track keys that have been written so that we can read them in the future
				t.handleWrittenKey(keyIndex)
			case t.writeInfoChan <- nextWriteInfo:
				// prepare a value to be eventually written
				nextWriteInfo = t.generateNextWriteInfo()
			case <-ticker.C:
				// perform garbage collection on cohorts
				t.DoCohortGC()
			}

			nextReadInfo = t.generateNextReadInfo()

		} else {
			// Standard case.

			select {
			case <-t.errorMonitor.ImmediateShutdownRequired():
				return
			case <-t.ctx.Done():
				return
			case keyIndex := <-t.writtenKeyIndicesChan:
				// track keys that have been written so that we can read them in the future
				t.handleWrittenKey(keyIndex)
			case t.writeInfoChan <- nextWriteInfo:
				// prepare a value to be eventually written
				nextWriteInfo = t.generateNextWriteInfo()
			case t.readInfoChan <- nextReadInfo:
				// prepare a value to be eventually read
				nextReadInfo = t.generateNextReadInfo()
			case <-ticker.C:
				// perform garbage collection on cohorts
				t.DoCohortGC()
			}
		}
	}
}

// handleWrittenKey handles a key that has been written to the database.
func (t *DataTracker) handleWrittenKey(keyIndex uint64) {
	// Add key index to the set of written keys we are tracking.
	t.writtenKeysSet[keyIndex] = struct{}{}

	// Determine the highest key index written so far that also has all lower key indices written.
	for {
		nextKeyIndex := uint64(t.highestWrittenKeyIndex + 1)
		if _, ok := t.writtenKeysSet[nextKeyIndex]; ok {
			// The next key has been written, mark it as such.
			t.highestWrittenKeyIndex = int64(nextKeyIndex)
			delete(t.writtenKeysSet, nextKeyIndex)
		} else {
			// Once we find the first key that has not been written, we can stop checking.
			// We want t.highestWrittenKeyIndex to be the highest key index that has been written
			// without any gaps in the sequence.
			break
		}
	}

	// Determine the highest cohort index written so far that also has all lower cohorts written.
	for {
		nextCohortIndex := uint64(t.highestWrittenCohortIndex + 1)
		if nextCohortIndex >= t.activeCohort.CohortIndex() {
			// Don't ever mark the active cohort as complete.
			break
		}
		nextCohort := t.cohorts[nextCohortIndex]
		if int64(nextCohort.HighKeyIndex()) <= t.highestWrittenKeyIndex {
			// We've found a cohort that has all keys written.
			t.highestWrittenCohortIndex = int64(nextCohort.CohortIndex())
			t.completeCohortSet[nextCohort.CohortIndex()] = struct{}{}
			err := nextCohort.MarkComplete()
			if err != nil {
				t.errorMonitor.Panic(fmt.Errorf("failed to mark cohort as complete: %v", err))
				return
			}
		} else {
			// Once we find the first cohort that does not have all keys written, we can stop checking.
			break
		}
	}
}

// generateNextWriteInfo generates the next write info to be placed into the writeInfoChan.
func (t *DataTracker) generateNextWriteInfo() *WriteInfo {
	var err error

	if t.activeCohort.IsExhausted() {
		t.activeCohort, err = t.cohorts[t.highestCohortIndex].NextCohort(t.config.CohortSize, t.valueSize)
		if err != nil {
			t.errorMonitor.Panic(fmt.Errorf("failed to generate next cohort for highest cohort: %v", err))
			return nil
		}
		t.highestCohortIndex = t.activeCohort.CohortIndex()
		t.cohorts[t.highestCohortIndex] = t.activeCohort
	}

	keyIndex, err := t.activeCohort.GetKeyIndexForWriting()
	if err != nil {
		t.errorMonitor.Panic(fmt.Errorf("failed to get key index for writing: %v", err))
		return nil
	}

	return &WriteInfo{
		KeyIndex: keyIndex,
		Key:      t.generator.Key(keyIndex),
		Value:    t.generator.Value(keyIndex, t.activeCohort.valueSize),
	}
}

// generateNextReadInfo generates the next read info to be placed into the readInfoChan.
func (t *DataTracker) generateNextReadInfo() *ReadInfo {
	if len(t.completeCohortSet) == 0 {
		// No cohorts are complete, so we can't read anything.
		return nil
	}

	var cohortIndexToRead uint64
	for cohortIndexToRead = range t.completeCohortSet {
		// map iteration is random in golang, so this will yield a random complete cohort.
		break
	}
	cohortToRead := t.cohorts[cohortIndexToRead]

	keyIndex, err := cohortToRead.GetKeyIndexForReading(t.rand)
	if err != nil {
		t.errorMonitor.Panic(fmt.Errorf("failed to get key index for reading: %v", err))
		return nil
	}

	return &ReadInfo{
		Key:   t.generator.Key(keyIndex),
		Value: t.generator.Value(keyIndex, cohortToRead.ValueSize()),
	}
}

// DoCohortGC performs garbage collection on the cohorts, removing cohorts with entries that are nearing expiration.
func (t *DataTracker) DoCohortGC() {
	now := time.Now()

	// Check all cohorts except for the active cohort (i.e. the one with index t.highestCohortIndex).
	for i := t.lowestCohortIndex; i < t.highestCohortIndex; i++ {
		cohort := t.cohorts[i]

		if cohort.IsExpired(now, t.safeTTL) {
			err := cohort.Delete()
			if err != nil {
				t.errorMonitor.Panic(fmt.Errorf("failed to delete expired cohort: %v", err))
				return
			}
			t.lowestCohortIndex++
			delete(t.cohorts, cohort.CohortIndex())
			delete(t.completeCohortSet, cohort.CohortIndex())
		} else {
			// Stop once we find the first cohort that is not eligible for deletion.
			break
		}
	}

	if len(t.cohorts) == 0 {
		// Edge case: we've been writing data slow enough that the active cohort has expired.
		// Create a new active cohort.
		activeCohort, err := t.activeCohort.NextCohort(t.config.CohortSize, t.valueSize)
		if err != nil {
			t.errorMonitor.Panic(fmt.Errorf("failed to create new active cohort: %v", err))
			return
		}

		t.activeCohort = activeCohort
		t.highestCohortIndex = activeCohort.CohortIndex()
		t.cohorts[activeCohort.CohortIndex()] = activeCohort
	}
}
