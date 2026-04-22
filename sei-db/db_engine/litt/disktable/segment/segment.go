package segment

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"sync/atomic"
	"time"

	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// unflushedKeysInitialCapacity is the initial capacity of the unflushedKeys slice. This slice is used to store keys
// that have been written to the segment but have not yet been flushed to disk.
const unflushedKeysInitialCapacity = 128

// shardControlChannelCapacity is the capacity of the channel used to send messages to the shard control loop.
const shardControlChannelCapacity = 32

// Segment is a chunk of data stored on disk. All data in a particular data segment is expired at the same time.
//
// This struct is not safe for operations that mutate the segment, access control must be handled by the caller.
type Segment struct {
	// The logger for the segment.
	logger logging.Logger

	// Used to signal an unrecoverable error in the segment. If errorMonitor.Panic() is called, the entire DB
	// enters a "panic" state and will refuse to do additional work.
	errorMonitor *util.ErrorMonitor

	// The index of the data segment. The first data segment ever created has index 0, the next has index 1, and so on.
	index uint32

	// This file contains metadata about the segment.
	metadata *metadataFile

	// This file contains the keys for the data segment, and is used for performing garbage collection on the key index.
	keys *keyFile

	// The value files, one for each shard in the segment. Indexed by shard number.
	shards []*valueFile

	// shardSizes is a list of the current sizes of each shard in the segment. Indexed by shard number. This
	// value is only tracked for mutable segments (i.e. the unsealed segment), meaning that if this segment was loaded
	// from disk, the values in this slice will be zero.
	shardSizes []uint64

	// The current size of the key file in bytes. This is only tracked for mutable segments, meaning that if this
	// segment was loaded from disk, this value will be zero.
	keyFileSize uint64

	// The maximum size of all shards in this segment.
	maxShardSize uint64

	// The number of keys written to this segment.
	keyCount uint32

	// shardChannels is a list of channels used to send messages to the goroutine responsible for writing to
	// each shard. Indexed by shard number.
	shardChannels []chan any

	// keyFileChannel is a channel used to send messages to the goroutine responsible for writing to the key file.
	keyFileChannel chan any

	// deletionChannel permits a caller to block until this segment is fully deleted. An element is inserted into
	// the channel when the segment is fully deleted.
	deletionChannel chan struct{}

	// reservationCount is the number of reservations on this segment. The segment will not be deleted until this count
	// reaches zero.
	reservationCount atomic.Int32

	// nextSegment is the next segment in the chain (i.e. the segment with index+1). Each segment takes a reservation
	// on the next segment in the sequence. This reservation is released when the segment is fully deleted. This
	// ensures that segments are always deleted strictly in sequence. This makes it impossible for a crash to cause
	// segment X to be missing while segment X-1 is present.
	nextSegment *Segment

	// Used as a sanity checker. For each value written to the segment, the segment must eventually return
	// a key to be written to the keymap. This value tracks the number of values that have been written to the
	// segment but have not yet been flushed to the keymap. When the segment is eventually sealed, the code
	// asserts that this value is zero. This check should never fail, but is a nice safety net.
	unflushedKeyCount atomic.Int64

	// If true, then take a snapshot of the segment when it is sealed.
	snapshottingEnabled bool

	// If true, then sync the file system for atomic operations. Should always be true in production, but can
	// be set to false for tests to save time.
	fsync bool
}

// CreateSegment creates a new data segment.
func CreateSegment(
	logger logging.Logger,
	errorMonitor *util.ErrorMonitor,
	index uint32,
	segmentPaths []*SegmentPath,
	snapshottingEnabled bool,
	shardingFactor uint32,
	salt [16]byte,
	fsync bool) (*Segment, error) {

	if len(segmentPaths) == 0 {
		return nil, errors.New("no segment paths provided")
	}

	metadata, err := createMetadataFile(index, shardingFactor, salt, segmentPaths[0], fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata file: %v", err)
	}

	keys, err := createKeyFile(logger, index, segmentPaths[0], false)
	if err != nil {
		return nil, fmt.Errorf("failed to open key file: %v", err)
	}

	keyFileSize := keys.Size()

	shards := make([]*valueFile, metadata.shardingFactor)
	for shard := uint32(0); shard < metadata.shardingFactor; shard++ {
		// Assign value files to available segment paths in a round-robin fashion.
		// Assign the first shard to the directory at index 1. The first directory
		// is used by the keymap, so if we have enough directories we don't want to
		// use it for value files too.
		segmentPath := segmentPaths[int(shard+1)%len(segmentPaths)]

		values, err := createValueFile(logger, index, shard, segmentPath, fsync)
		if err != nil {
			return nil, fmt.Errorf("failed to open value file: %v", err)
		}
		shards[shard] = values
	}

	shardSizes := make([]uint64, metadata.shardingFactor)

	shardChannels := make([]chan any, metadata.shardingFactor)
	for shard := uint32(0); shard < metadata.shardingFactor; shard++ {
		shardChannels[shard] = make(chan any, shardControlChannelCapacity)
	}

	// If at all possible, we want to size this channel so that the goroutines writing data to the sharded value files
	// do not block on insertion into this channel. Scale the size of this channel by the number of shards, as more
	// shards mean there may be a higher rate of writes to this channel.
	keyFileChannel := make(chan any, shardControlChannelCapacity*metadata.shardingFactor)

	segment := &Segment{
		logger:              logger,
		errorMonitor:        errorMonitor,
		index:               index,
		metadata:            metadata,
		keys:                keys,
		shards:              shards,
		shardSizes:          shardSizes,
		keyFileSize:         keyFileSize,
		shardChannels:       shardChannels,
		keyFileChannel:      keyFileChannel,
		deletionChannel:     make(chan struct{}, 1),
		snapshottingEnabled: snapshottingEnabled,
		fsync:               fsync,
	}

	// Segments are returned with an initial reference count of 1, as the caller of the constructor is considered to
	// have a reference to the segment.
	segment.reservationCount.Store(1)

	// Start up the control loops.
	for shard := uint32(0); shard < metadata.shardingFactor; shard++ {
		go segment.shardControlLoop(shard)
	}

	go segment.keyFileControlLoop()

	return segment, nil
}

// LoadSegment loads an existing segment from disk. If that segment is unsealed, this method will seal it.
func LoadSegment(logger logging.Logger,
	errorMonitor *util.ErrorMonitor,
	index uint32,
	segmentPaths []*SegmentPath,
	snapshottingEnabled bool,
	now time.Time,
	fsync bool,
) (*Segment, error) {

	if len(segmentPaths) == 0 {
		return nil, errors.New("no segment paths provided")
	}

	// Look for the metadata file.
	metadata, err := loadMetadataFile(index, segmentPaths, fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata file: %w", err)
	}

	// Look for the key file.
	keys, err := loadKeyFile(logger, index, segmentPaths, metadata.segmentVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to open key file: %v", err)
	}
	keyFileSize := keys.Size()

	// Look for the value files. There should be one for each shard.
	shards := make([]*valueFile, metadata.shardingFactor)
	for shard := uint32(0); shard < metadata.shardingFactor; shard++ {
		values, err := loadValueFile(logger, index, shard, segmentPaths)
		if err != nil {
			return nil, fmt.Errorf("failed to open value file: %v", err)
		}
		shards[shard] = values
	}

	segment := &Segment{
		logger:              logger,
		errorMonitor:        errorMonitor,
		index:               index,
		metadata:            metadata,
		keys:                keys,
		shards:              shards,
		keyFileSize:         keyFileSize,
		keyCount:            metadata.keyCount,
		deletionChannel:     make(chan struct{}, 1),
		snapshottingEnabled: snapshottingEnabled,
		fsync:               fsync,
	}

	// Segments are returned with an initial reference count of 1, as the caller of the constructor is considered to
	// have a reference to the segment.
	segment.reservationCount.Store(1)

	if !metadata.sealed {
		err = segment.sealLoadedSegment(now)
		if err != nil {
			return nil, fmt.Errorf("failed to seal segment: %w", err)
		}
	}

	return segment, nil
}

// SegmentIndex returns the index of the segment.
func (s *Segment) SegmentIndex() uint32 {
	return s.index
}

// sealLoadedSegment is responsible for sealing a segment loaded from disk that is not already sealed.
// While doing this, it is responsible for making the key file consistent with the values present in the
// value files.
func (s *Segment) sealLoadedSegment(now time.Time) error {
	scopedKeys, err := s.keys.readKeys()
	if err != nil {
		return fmt.Errorf("failed to read keys: %w", err)
	}

	// keys with values that are not present in the value files
	goodKeys := make([]*types.ScopedKey, 0, len(scopedKeys))

	// keys with values that weren't flushed out to the value files before the DB crashed
	badKeys := make([]*types.ScopedKey, 0, len(scopedKeys))

	for _, scopedKey := range scopedKeys {
		shard := s.GetShard(scopedKey.Key)

		requiredValueFileLength := uint64(scopedKey.Address.Offset()) +
			4 /* value size uint32 */ +
			uint64(scopedKey.ValueSize)

		if s.shards[shard].Size() < requiredValueFileLength {
			badKeys = append(badKeys, scopedKey)
		} else {
			goodKeys = append(goodKeys, scopedKey)
		}
	}

	if len(badKeys) > 0 {
		// We have at least one bad key. Rewrite the keyfile with only the good keys.
		s.logger.Warnf("segment %d has %d unflushed value(s)",
			s.index, len(badKeys))

		swapFile, err := createKeyFile(s.logger, s.index, s.keys.segmentPath, true)
		if err != nil {
			return fmt.Errorf("failed to create swap key file: %w", err)
		}

		for _, scopedKey := range goodKeys {
			err = swapFile.write(scopedKey)
			if err != nil {
				return fmt.Errorf("failed to write key to swap file: %w", err)
			}
		}
		err = swapFile.seal()
		if err != nil {
			return fmt.Errorf("failed to seal swap file: %w", err)
		}

		err = swapFile.atomicSwap(s.fsync)
		if err != nil {
			return fmt.Errorf("failed to swap key file: %w", err)
		}

		s.keys = swapFile
	}

	err = s.metadata.seal(now, uint32(len(goodKeys)))
	if err != nil {
		return fmt.Errorf("failed to seal metadata file: %w", err)
	}
	s.keyCount = uint32(len(goodKeys))

	return nil
}

// Size returns the size of the segment in bytes. Counts bytes that are on disk or that will eventually end up on disk.
// This method is not thread safe, and should not be called concurrently with methods that modify the segment.
func (s *Segment) Size() uint64 {
	size := s.metadata.Size()

	if s.IsSealed() {
		// This segment is immutable, so it's thread safe to query the files directly.
		size += s.keys.Size()
		for _, shard := range s.shards {
			size += shard.Size()
		}
	} else {
		// This segment is mutable. We must use our local reckoning of the sizes of the files.
		size += s.keyFileSize
		for _, shardSize := range s.shardSizes {
			size += shardSize
		}
	}

	return size
}

// KeyCount returns the number of keys in the segment.
func (s *Segment) KeyCount() uint32 {
	return s.keyCount
}

// lookForFile looks for a file in a list of directories. It returns an error if the file appears
// in more than one directory, and nil if the file is not found. If the file is found and
// there are no errors, this method returns the SegmentPath where the file was found.
func lookForFile(paths []*SegmentPath, fileName string) (*SegmentPath, error) {
	locations := make([]*SegmentPath, 0, 1)
	for _, possiblePath := range paths {
		potentialLocation := path.Join(possiblePath.segmentDirectory, fileName)
		exists, err := util.Exists(potentialLocation)
		if err != nil {
			return nil, fmt.Errorf("failed to check if file %s exists: %v", potentialLocation, err)
		}
		if exists {
			locations = append(locations, possiblePath)
		}
	}

	if len(locations) > 1 {
		return nil, fmt.Errorf("file %s found in multiple directories: %v", fileName, locations)
	}

	if len(locations) == 0 {
		return nil, nil
	}
	return locations[0], nil
}

// SetNextSegment sets the next segment in the chain.
func (s *Segment) SetNextSegment(nextSegment *Segment) {
	nextSegment.Reserve()
	s.nextSegment = nextSegment
}

// GetShard returns the shard number for a key.
func (s *Segment) GetShard(key []byte) uint32 {
	if s.metadata.shardingFactor == 1 {
		// Shortcut: if we have one shard, we don't need to hash the key to figure out the mapping.
		return 0
	}

	if s.metadata.segmentVersion == OldHashFunctionSegmentVersion {
		return util.LegacyHashKey(key, s.metadata.legacySalt) % s.metadata.shardingFactor
	}

	hash := util.HashKey(key, s.metadata.salt)

	return hash % s.metadata.shardingFactor
}

// Write records a key-value pair in the data segment, returning the maximum size of all shards within this segment.
//
// This method does not ensure that the key-value pair is actually written to disk, only that it will eventually be
// written to disk. Flush must be called to ensure that all data previously passed to Write is written to disk.
func (s *Segment) Write(data *types.KVPair) (keyCount uint32, keyFileSize uint64, err error) {
	if s.metadata.sealed {
		return 0, 0, fmt.Errorf("segment is sealed, cannot write data")
	}

	shard := s.GetShard(data.Key)
	currentSize := s.shardSizes[shard]

	if currentSize > math.MaxUint32 {
		// No matter the configuration, we absolutely cannot permit a value to be written if the first byte of the
		// value would be beyond position 2^32. This is because we only have 32 bits in an address to store the
		// position of a value's first byte.
		return 0, 0,
			fmt.Errorf("value file already contains %d bytes, cannot add a new value", currentSize)
	}
	s.unflushedKeyCount.Add(1)
	firstByteIndex := uint32(currentSize)

	s.shardSizes[shard] += uint64(len(data.Value)) + 4 /* uint32 length */
	if s.shardSizes[shard] > s.maxShardSize {
		s.maxShardSize = s.shardSizes[shard]
	}
	s.keyCount++
	s.keyFileSize += uint64(len(data.Key)) + 4 /* uint32 length */ + 8 /* uint64 Address */ + 4 /* uint32 ValueSize */

	// Forward the value to the shard control loop, which asynchronously writes it to the value file.
	shardRequest := &valueToWrite{
		value:                  data.Value,
		expectedFirstByteIndex: firstByteIndex,
	}
	err = util.Send(s.errorMonitor, s.shardChannels[shard], shardRequest)
	if err != nil {
		return 0, 0,
			fmt.Errorf("failed to send value to shard control loop: %v", err)
	}

	// Forward the value to the key and its address file control loop, which asynchronously writes it to the key file.
	keyRequest := &types.ScopedKey{
		Key:       data.Key,
		Address:   types.NewAddress(s.index, firstByteIndex),
		ValueSize: uint32(len(data.Value)),
	}

	err = util.Send(s.errorMonitor, s.keyFileChannel, keyRequest)
	if err != nil {
		return 0, 0,
			fmt.Errorf("failed to send key to key file control loop: %v", err)
	}

	return s.keyCount, s.keyFileSize, nil
}

// GetMaxShardSize returns the maximum size of all shards in this segment.
func (s *Segment) GetMaxShardSize() uint64 {
	return s.maxShardSize
}

// Read fetches the data for a key from the data segment.
//
// It is only thread safe to read from a segment if the key being read has previously been flushed to disk.
func (s *Segment) Read(key []byte, dataAddress types.Address) ([]byte, error) {
	shard := s.GetShard(key)
	values := s.shards[shard]

	value, err := values.read(dataAddress.Offset())
	if err != nil {
		return nil, fmt.Errorf("failed to read value: %w", err)
	}
	return value, nil
}

// GetKeys returns all keys in the data segment. Only permitted to be called after the segment has been sealed.
func (s *Segment) GetKeys() ([]*types.ScopedKey, error) {
	if !s.metadata.sealed {
		return nil, fmt.Errorf("segment is not sealed, cannot read keys")
	}

	keys, err := s.keys.readKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to read keys: %w", err)
	}
	return keys, nil
}

// FlushWaitFunction is a function that waits for a flush operation to complete. It returns the addresses of the data
// that was flushed, or an error if the flush operation failed.
type FlushWaitFunction func() ([]*types.ScopedKey, error)

// Flush schedules a flush operation. Flush operations are performed serially in the order they are scheduled.
// This method returns a function that, when called, will block until the flush operation is complete. The function
// returns the addresses of the data that was flushed, or an error if the flush operation failed.
func (s *Segment) Flush() (FlushWaitFunction, error) {
	return s.flush(false)
}

func (s *Segment) flush(seal bool) (FlushWaitFunction, error) {
	// Schedule a flush for all shards.
	shardResponseChannels := make([]chan struct{}, s.metadata.shardingFactor)
	for shard, shardChannel := range s.shardChannels {
		shardResponseChannels[shard] = make(chan struct{}, 1)
		request := &shardFlushRequest{
			seal:              seal,
			completionChannel: shardResponseChannels[shard],
		}
		err := util.Send(s.errorMonitor, shardChannel, request)
		if err != nil {
			return nil, fmt.Errorf("failed to send flush request to shard %d: %w", shard, err)
		}
	}

	// Schedule a flush for the key channel.
	// Now that all shards have sent their key/address pairs to the key file, flush the key file.
	keyResponseChannel := make(chan *keyFileFlushResponse, 1)
	request := &keyFileFlushRequest{
		seal:              seal,
		completionChannel: keyResponseChannel,
	}
	err := util.Send(s.errorMonitor, s.keyFileChannel, request)
	if err != nil {
		return nil, fmt.Errorf("failed to send flush request to key file: %w", err)
	}

	return func() ([]*types.ScopedKey, error) {
		// Wait for each shard to finish flushing.
		for i := range s.shardChannels {
			_, err := util.Await(s.errorMonitor, shardResponseChannels[i])
			if err != nil {
				return nil, fmt.Errorf("failed to flush shard %d: %w", i, err)
			}
		}

		keyFlushResponse, err := util.Await(s.errorMonitor, keyResponseChannel)
		if err != nil {
			return nil, fmt.Errorf("failed to flush key file: %w", err)
		}

		s.unflushedKeyCount.Add(-int64(len(keyFlushResponse.addresses)))
		return keyFlushResponse.addresses, nil
	}, nil
}

// Snapshot takes a snapshot of the files in the segment if snapshotting is enabled. If snapshotting is not enabled,
// then this method is a no-op.
func (s *Segment) Snapshot() error {
	if !s.snapshottingEnabled {
		return nil
	}

	err := s.metadata.snapshot()
	if err != nil {
		return fmt.Errorf("failed to snapshot metadata file: %w", err)
	}

	err = s.keys.snapshot()
	if err != nil {
		return fmt.Errorf("failed to snapshot key file: %w", err)
	}

	for shardIndex, shard := range s.shards {
		err = shard.snapshot()
		if err != nil {
			return fmt.Errorf("failed to snapshot value file for shard %d: %w", shardIndex, err)
		}
	}

	return nil
}

// Check if this segment is actually a snapshot. A snapshot will be backed up by symlinks, while a real segment
// will have real files.
func (s *Segment) IsSnapshot() (bool, error) {
	metadataPath := s.metadata.path()

	fileInfo, err := os.Lstat(metadataPath)
	if err != nil {
		return false, fmt.Errorf("failed to get file info for metadata path %s: %w", metadataPath, err)
	}

	return fileInfo.Mode()&os.ModeSymlink != 0, nil
}

// Seal flushes all data to disk and finalizes the metadata. Returns addresses that became durable as a result of
// this method call. After this method is called, no more data can be written to this segment.
func (s *Segment) Seal(now time.Time) ([]*types.ScopedKey, error) {
	flushWaitFunction, err := s.flush(true)
	if err != nil {
		return nil, fmt.Errorf("failed to flush segment: %w", err)
	}
	addresses, err := flushWaitFunction()
	if err != nil {
		return nil, fmt.Errorf("failed to flush segment: %w", err)
	}

	// Seal the metadata file.
	err = s.metadata.seal(now, s.keyCount)
	if err != nil {
		return nil, fmt.Errorf("failed to seal metadata file: %w", err)
	}

	unflushedKeyCount := s.unflushedKeyCount.Load()
	if s.unflushedKeyCount.Load() != 0 {
		return nil, fmt.Errorf("segment %d has %d unflushedKeyCount keys", s.index, unflushedKeyCount)
	}

	return addresses, nil
}

// IsSealed returns true if the segment is sealed, and false otherwise.
func (s *Segment) IsSealed() bool {
	return s.metadata.sealed
}

// GetSealTime returns the time at which the segment was sealed. If the file is not sealed, this method will return
// the zero time.
func (s *Segment) GetSealTime() time.Time {
	return time.Unix(0, int64(s.metadata.lastValueTimestamp))
}

// Reserve reserves the segment, preventing it from being deleted. Returns true if the reservation was successful, and
// false otherwise.
func (s *Segment) Reserve() bool {
	for {
		reservations := s.reservationCount.Load()
		if reservations <= 0 {
			return false
		}

		if s.reservationCount.CompareAndSwap(reservations, reservations+1) {
			return true
		}
	}
}

// Release releases a reservation held on this segment. A segment cannot be deleted until all reservations on it
// have been released. The last call to Release() that releases the final reservation schedules the segment for
// asynchronous deletion
func (s *Segment) Release() {
	reservations := s.reservationCount.Add(-1)

	if reservations > 0 {
		return
	}

	if reservations < 0 {
		// This should be impossible.
		s.errorMonitor.Panic(
			fmt.Errorf("segment %d has negative reservation count: %d", s.index, reservations))
	}

	go func() {
		err := s.delete()
		if err != nil {
			s.errorMonitor.Panic(fmt.Errorf("failed to delete segment: %w", err))
		}
	}()
}

// BlockUntilFullyDeleted blocks until the segment is fully deleted. If the segment is not yet fully released,
// this method will block until it is. This method should only be called once per segment (the second call
// will block forever!).
func (s *Segment) BlockUntilFullyDeleted() error {
	_, err := util.Await(s.errorMonitor, s.deletionChannel)
	if err != nil {
		return fmt.Errorf("failed to await segment deletion: %w", err)
	}
	return nil
}

// delete deletes the segment from disk.
func (s *Segment) delete() error {
	defer func() {
		s.deletionChannel <- struct{}{}
	}()

	err := s.keys.delete()
	if err != nil {
		return fmt.Errorf("failed to delete key file, segment %d: %w", s.index, err)
	}
	for shardIndex, shard := range s.shards {
		err = shard.delete()
		if err != nil {
			return fmt.Errorf("failed to delete value file, segment %d, shard %d: %w", s.index, shardIndex, err)
		}
	}
	err = s.metadata.delete()
	if err != nil {
		return fmt.Errorf("failed to delete metadata file, segment %d: %w", s.index, err)
	}

	// The next segment is now eligible for deletion once it is fully released by other reservation holders.
	if s.nextSegment != nil {
		s.nextSegment.Release()
	}

	return nil
}

func (s *Segment) String() string {
	var sealedString string
	if s.metadata.sealed {
		sealedString = "sealed"
	} else {
		sealedString = "unsealed"
	}

	return fmt.Sprintf("[seg %d - %s]", s.index, sealedString)
}

// handleShardFlushRequest handles a request to flush a shard to disk.
func (s *Segment) handleShardFlushRequest(shard uint32, request *shardFlushRequest) {
	if request.seal {
		err := s.shards[shard].seal()
		if err != nil {
			s.errorMonitor.Panic(fmt.Errorf("failed to seal value file: %w", err))
		}
	} else {
		err := s.shards[shard].flush()
		if err != nil {
			s.errorMonitor.Panic(fmt.Errorf("failed to flush value file: %w", err))
		}
	}
	request.completionChannel <- struct{}{}
}

// handleShardWrite applies a single write operation to a shard.
func (s *Segment) handleShardWrite(shard uint32, data *valueToWrite) {
	firstByteIndex, err := s.shards[shard].write(data.value)
	if err != nil {
		s.errorMonitor.Panic(fmt.Errorf("failed to write value to value file: %w", err))
	}

	if firstByteIndex != data.expectedFirstByteIndex {
		// This should never happen. But it's a good sanity check.
		if firstByteIndex != data.expectedFirstByteIndex {
			s.errorMonitor.Panic(
				fmt.Errorf("expected first byte index %d, got %d", data.expectedFirstByteIndex, firstByteIndex))
		}
	}
}

// handleKeyFileWrite writes a key to the key file.
func (s *Segment) handleKeyFileWrite(data *types.ScopedKey) {
	err := s.keys.write(data)
	if err != nil {
		s.errorMonitor.Panic(fmt.Errorf("failed to write key to key file: %w", err))
	}
}

// handleKeyFileFlushRequest handles a request to flush the key file to disk.
func (s *Segment) handleKeyFileFlushRequest(request *keyFileFlushRequest, unflushedKeys []*types.ScopedKey) {
	if request.seal {
		err := s.keys.seal()
		if err != nil {
			s.errorMonitor.Panic(fmt.Errorf("failed to seal key file: %w", err))
		}
	} else {
		err := s.keys.flush()
		if err != nil {
			s.errorMonitor.Panic(fmt.Errorf("failed to flush key file: %w", err))
		}
	}

	request.completionChannel <- &keyFileFlushResponse{
		addresses: unflushedKeys,
	}
}

// shardFlushRequest is a message sent to shard control loops to request that they flush their data to disk.
type shardFlushRequest struct {
	// If true, seal the shard after flushing. If false, do not seal the shard.
	seal bool

	// As each shard finishes its flush it will send an object to this channel.
	completionChannel chan struct{}
}

// valueToWrite is a message sent to the shard control loop to request that it write a value to the value file.
type valueToWrite struct {
	value                  []byte
	expectedFirstByteIndex uint32
}

// shardControlLoop is the main loop for performing modifications to a particular shard. Each shard is managed
// by its own goroutine, which is running this function.
func (s *Segment) shardControlLoop(shard uint32) {
	for {
		select {
		case <-s.errorMonitor.ImmediateShutdownRequired():
			s.logger.Infof("segment %d shard %d control loop exiting, context cancelled", s.index, shard)
			return
		case operation := <-s.shardChannels[shard]:
			if flushRequest, ok := operation.(*shardFlushRequest); ok {
				s.handleShardFlushRequest(shard, flushRequest)
				if flushRequest.seal {
					// After sealing, we can exit the control loop.
					return
				}
			} else if data, ok := operation.(*valueToWrite); ok {
				s.handleShardWrite(shard, data)
				continue
			} else {
				s.errorMonitor.Panic(
					fmt.Errorf("unknown operation type in shard control loop: %T", operation))
			}
		}
	}
}

// keyFileFlushRequest is a message sent to the key file control loop to request that it flush its data to disk.
type keyFileFlushRequest struct {
	// If true, seal the key file after flushing. If false, do not seal the key file.
	seal bool

	// As the key file finishes its flush, it will either send an error if something went wrong, or nil if the flush was
	// successful.
	completionChannel chan *keyFileFlushResponse
}

// keyFileFlushResponse is a message sent from the key file control loop to the caller of Flush to indicate that the
// key file has been flushed.
type keyFileFlushResponse struct {
	addresses []*types.ScopedKey
}

// keyFileControlLoop is the main loop for performing modifications to the key file. This goroutine is responsible
// for writing key-address pairs to the key file.
func (s *Segment) keyFileControlLoop() {
	unflushedKeys := make([]*types.ScopedKey, 0, unflushedKeysInitialCapacity)

	for {
		select {
		case <-s.errorMonitor.ImmediateShutdownRequired():
			s.logger.Infof("segment %d key file control loop exiting, context cancelled", s.index)
			return
		case operation := <-s.keyFileChannel:

			if flushRequest, ok := operation.(*keyFileFlushRequest); ok {
				s.handleKeyFileFlushRequest(flushRequest, unflushedKeys)
				unflushedKeys = make([]*types.ScopedKey, 0, unflushedKeysInitialCapacity)

				if flushRequest.seal {
					// After sealing, we can exit the control loop.
					return
				}

			} else if data, ok := operation.(*types.ScopedKey); ok {
				s.handleKeyFileWrite(data)
				unflushedKeys = append(unflushedKeys, data)

			} else {
				s.errorMonitor.Panic(
					fmt.Errorf("unknown operation type in key file control loop: %T", operation))
			}
		}
	}
}

// GetMetadataFilePath returns the path to the metadata file for this segment.
func (s *Segment) GetMetadataFilePath() string {
	return s.metadata.path()
}

// GetKeyFilePath returns the path to the key file for this segment.
func (s *Segment) GetKeyFilePath() string {
	return s.keys.path()
}

// / GetValueFilePaths returns a list of file paths for all value files in this segment.
func (s *Segment) GetValueFilePaths() []string {
	paths := make([]string, 0, len(s.shards))
	for _, shard := range s.shards {
		paths = append(paths, shard.path())
	}
	return paths
}

// GetFilePaths returns a list of file paths for all files that make up this segment.
func (s *Segment) GetFilePaths() []string {
	filePaths := make([]string, 0, 2+len(s.shards))
	filePaths = append(filePaths, s.GetMetadataFilePath())
	filePaths = append(filePaths, s.GetKeyFilePath())
	filePaths = append(filePaths, s.GetValueFilePaths()...)
	return filePaths
}
