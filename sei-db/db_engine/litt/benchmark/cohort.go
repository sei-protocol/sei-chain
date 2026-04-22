package benchmark

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Layr-Labs/eigenda/litt/util"
)

// CohortFileExtension is the file extension used for cohort files.
const CohortFileExtension = ".cohort"

// CohortSwapFileExtension is the file extension used for cohort swap files. Used to atomically update cohort files.
const CohortSwapFileExtension = CohortFileExtension + util.SwapFileExtension

/* The lifecycle of a cohort:

    +-----+     +-----------+     +----------+     +---------+
    | new | --> | exhausted | --> | complete | --> | expired |
    +-----+     +-----------+     +----------+     +---------+
       |              |
       v              |
    +-----------+     |
    | abandoned | <---|
    +-----------+

- new: the cohort was just created and is currently being used to supply keys for writing.
- exhausted: all keys in the cohort have been scheduled for writing, but the DB may not have ingested them all yet.
- complete: all keys in the cohort have been written to the DB and are safe to read.
- abandoned: before becoming complete, the benchmark was restarted. It will never be thread safe to read or write
              any keys in this cohort.
- expired: the cohort has been marked as complete, but it can no longer be read because the TTL has expired
            (or is about to expire).
*/

// A Cohort is a grouping of key-value pairs used for benchmarking.
//
// If a benchmark wants to read values, it must somehow figure out which keys have been written to the database.
// If it wants to verify the validity of the data it reads, it must also be able to determine the correct value
// that should be associated with any particular key, and it must also be able to determine when keys are
// expected to be removed from the database due to TTL expiration.
//
// Tracking the sort of metadata required to do reads in a benchmark is not a trivial thing, especially when
// the scale of the benchmark is large (i.e. tens or hundreds of millions of keys over weeks or months of time).
// Storing this information in memory is simply not plausible, and storing it on disk requires database scale similar
// to what LittDB is handling, unless we are clever about it. A "cohort" is that clever mechanism. Each cohort tracks a
// large collection of key-value pairs in the database, and it does it in a way that uses very little disk space.
//
// Key-value pairs each have unique indices, and knowing the index of a key-value pair allows the data to be
// regenerated deterministically. All key-value pairs in a cohort have sequential indices. A single cohort can
// track multiple gigabytes worth of key-value pairs, but on disk it only requires a few dozen bytes of data.
type Cohort struct {
	// The directory where the cohort file is stored.
	parentDirectory string

	// The unique ID of this cohort.
	cohortIndex uint64

	// The index of the first key-value pair in the cohort.
	lowKeyIndex uint64

	// The index of the last key-value pair in the cohort.
	highKeyIndex uint64

	// The size of the values written in this cohort.
	valueSize uint64

	// The next available index to be written. Only relevant for a new cohort that is currently being written to
	// the DB. This value is undefined for cohorts that have been completely written or loaded from disk. This value
	// is NOT serialized to disk.
	nextKeyIndex uint64

	// True iff all key-value pairs in the cohort have been written to the database.
	allValuesWritten bool

	// A timestamp that is guaranteed to come before the first value in the cohort is written to the database.
	firstValueTimestamp time.Time

	// True iff the cohort has been loaded from disk. This value is NOT serialized to disk.
	loadedFromDisk bool

	// Whether fsync mode is enabled. Disable for faster unit tests.
	fsync bool
}

// NewCohort creates a new cohort with the given index range.
func NewCohort(
	parentDirectory string,
	cohortIndex uint64,
	lowIndex uint64,
	highIndex uint64,
	valueSize uint64,
	fsync bool) (*Cohort, error) {

	cohort := &Cohort{
		parentDirectory:     parentDirectory,
		cohortIndex:         cohortIndex,
		lowKeyIndex:         lowIndex,
		highKeyIndex:        highIndex,
		valueSize:           valueSize,
		nextKeyIndex:        lowIndex,
		allValuesWritten:    false,
		firstValueTimestamp: time.Now(),
		fsync:               fsync,
	}

	err := cohort.Write()
	if err != nil {
		return nil, fmt.Errorf("failed to write cohort file: %w", err)
	}

	return cohort, nil
}

// LoadCohort loads a cohort from the given path.
func LoadCohort(path string) (*Cohort, error) {

	parentDirectory := filepath.Dir(path)
	// Cohort file names are in the format "X.cohort", where X is the cohort index.
	// Replacing ".cohort" with an empty string gives us the cohort index in string form.
	indexString := strings.Replace(filepath.Base(path), CohortFileExtension, "", 1)
	cohortIndex, err := strconv.ParseUint(indexString, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cohort file %s: %w", path, err)
	}

	cohort := &Cohort{
		parentDirectory: parentDirectory,
		cohortIndex:     cohortIndex,
		loadedFromDisk:  true,
	}

	filePath := cohort.Path()
	if err = util.ErrIfNotExists(filePath); err != nil {
		return nil, fmt.Errorf("cohort file does not exist: %s", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cohort file: %w", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cohort file: %w", err)
	}

	err = cohort.deserialize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize cohort file: %w", err)
	}

	err = file.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close cohort file: %w", err)
	}

	return cohort, nil
}

// NextCohort creates the next cohort in the sequence with the given number of keys.
func (c *Cohort) NextCohort(keyCount uint64, valueSize uint64) (*Cohort, error) {
	nextIndex := c.cohortIndex + 1
	nextLowKeyIndex := c.highKeyIndex + 1
	nextHighKeyIndex := nextLowKeyIndex + keyCount - 1

	nextCohort, err := NewCohort(
		c.parentDirectory,
		nextIndex,
		nextLowKeyIndex,
		nextHighKeyIndex,
		valueSize,
		c.fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to create next cohort: %w", err)
	}
	return nextCohort, nil
}

// CohortIndex returns the index of the cohort.
func (c *Cohort) CohortIndex() uint64 {
	return c.cohortIndex
}

// LowKeyIndex returns the index of the first key in the cohort.
func (c *Cohort) LowKeyIndex() uint64 {
	return c.lowKeyIndex
}

// HighKeyIndex returns the index of the last key in the cohort.
func (c *Cohort) HighKeyIndex() uint64 {
	return c.highKeyIndex
}

func (c *Cohort) ValueSize() uint64 {
	return c.valueSize
}

// FirstValueTimestamp returns the timestamp of the first value in the cohort.
func (c *Cohort) FirstValueTimestamp() time.Time {
	return c.firstValueTimestamp
}

// IsComplete returns true if all key-value pairs in the cohort have been written to the database. Only complete
// cohorts are safe to read from.
func (c *Cohort) IsComplete() bool {
	return c.allValuesWritten
}

// IsExhausted returns true if the cohort has been exhausted, i.e. it has produced all keys for writing that it is
// capable of producing. Once exhausted, a cohort should be marked as completed once all key-value pairs have been
// written to the database, thus making all keys in the cohort safe to read.
func (c *Cohort) IsExhausted() bool {
	return c.nextKeyIndex > c.highKeyIndex
}

// IsLoadedFromDisk returns true if the cohort has been loaded from disk.
func (c *Cohort) IsLoadedFromDisk() bool {
	return c.loadedFromDisk
}

// GetKeyIndexForWriting gets the next key to be written to the database.
func (c *Cohort) GetKeyIndexForWriting() (uint64, error) {
	if c.loadedFromDisk {
		return 0, fmt.Errorf("cannot allocate key for writing: cohort has been loaded from disk")
	}
	if c.allValuesWritten {
		return 0, fmt.Errorf("cannot allocate key for writing: cohort is already complete")
	}
	if c.IsExhausted() {
		return 0, fmt.Errorf("cannot allocate key for writing: cohort is exhausted")
	}

	key := c.nextKeyIndex
	c.nextKeyIndex++

	return key, nil
}

// GetKeyIndexForReading gets a random key from the cohort that is safe to read. This function should only be called
// after the cohort has been marked as complete.
func (c *Cohort) GetKeyIndexForReading(rand *rand.Rand) (uint64, error) {
	if !c.allValuesWritten {
		return 0, fmt.Errorf("cannot allocate key for reading: cohort is not complete")
	}

	choice := (rand.Uint64() % (c.highKeyIndex - c.lowKeyIndex + 1)) + c.lowKeyIndex

	// sanity check
	if choice < c.lowKeyIndex || choice > c.highKeyIndex {
		return 0, fmt.Errorf("invalid choice: %d not in range [%d, %d]", choice, c.lowKeyIndex, c.highKeyIndex)
	}

	return choice, nil
}

// MarkComplete marks that all key-value pairs in the cohort have been written to the database. Once done,
// all key-value pairs in the cohort become safe to read, so long as the cohort has not yet expired. A cohort
// is said to have expired when it is possible that at least one key in the cohort may be deleted from the DB
// due to the TTL.
func (c *Cohort) MarkComplete() error {
	if c.allValuesWritten {
		return fmt.Errorf("cannot mark cohort complete: cohort is already complete")
	}
	if c.loadedFromDisk {
		return fmt.Errorf("cannot mark cohort complete: cohort has been loaded from disk")
	}
	if c.nextKeyIndex <= c.highKeyIndex {
		return fmt.Errorf("cannot mark cohort complete: cohort is not exhausted")
	}

	c.allValuesWritten = true
	err := c.Write()
	if err != nil {
		return fmt.Errorf("failed to mark cohort complete: %w", err)
	}
	return nil
}

// Path returns the file path of the cohort file.
func (c *Cohort) Path() string {
	return path.Join(c.parentDirectory, fmt.Sprintf("%d%s", c.cohortIndex, CohortFileExtension))
}

// Write the data in this cohort to its file on disk. When this method returns, the cohort file is guaranteed to be
// crash durable.
func (c *Cohort) Write() error {
	err := util.AtomicWrite(c.Path(), c.serialize(), c.fsync)
	if err != nil {
		return fmt.Errorf("failed to write cohort file: %w", err)
	}

	return nil
}

// serialize serializes the cohort to a byte array.
func (c *Cohort) serialize() []byte {
	// Data size:
	//  - cohortIndex (8 bytes)
	//  - lowKeyIndex (8 bytes)
	//  - highKeyIndex (8 bytes)
	//  - valueSize (8 bytes)
	//  - firstValueTimestamp (8 bytes)
	//  - allValuesWritten (1 byte)
	// Total: 41 bytes

	data := make([]byte, 41)
	binary.BigEndian.PutUint64(data[0:8], c.cohortIndex)
	binary.BigEndian.PutUint64(data[8:16], c.lowKeyIndex)
	binary.BigEndian.PutUint64(data[16:24], c.highKeyIndex)
	binary.BigEndian.PutUint64(data[24:32], c.valueSize)
	binary.BigEndian.PutUint64(data[32:40], uint64(c.firstValueTimestamp.Unix()))
	if c.allValuesWritten {
		data[40] = 1
	} else {
		data[40] = 0
	}

	return data
}

func (c *Cohort) deserialize(data []byte) error {
	if len(data) != 41 {
		return fmt.Errorf("invalid data length: %d", len(data))
	}

	cohortIndex := binary.BigEndian.Uint64(data[0:8])
	if cohortIndex != c.cohortIndex {
		return fmt.Errorf("cohort index mismatch: %d != %d", cohortIndex, c.cohortIndex)
	}

	c.lowKeyIndex = binary.BigEndian.Uint64(data[8:16])
	c.highKeyIndex = binary.BigEndian.Uint64(data[16:24])
	c.valueSize = binary.BigEndian.Uint64(data[24:32])
	if c.lowKeyIndex >= c.highKeyIndex {
		return fmt.Errorf("invalid index range: %d >= %d", c.lowKeyIndex, c.highKeyIndex)
	}

	c.firstValueTimestamp = time.Unix(int64(binary.BigEndian.Uint64(data[32:40])), 0)
	c.allValuesWritten = data[40] == 1

	return nil
}

// IsExpired returns true if the cohort has expired (i.e. it is no longer safe to read).
func (c *Cohort) IsExpired(now time.Time, maxAge time.Duration) bool {
	if !c.IsComplete() {
		if c.loadedFromDisk {
			// Incomplete cohorts loaded from disk are instantly expired.
			return true
		} else {
			// A cohort currently in the process of being written can't expire.
			return false
		}
	}

	age := now.Sub(c.firstValueTimestamp)

	return age > maxAge
}

// Delete the associated cohort file.
func (c *Cohort) Delete() error {
	err := os.Remove(c.Path())
	if err != nil {
		return fmt.Errorf("failed to delete cohort file: %w", err)
	}
	return nil
}
