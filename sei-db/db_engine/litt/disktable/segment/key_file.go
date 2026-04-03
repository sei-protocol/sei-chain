package segment

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"strconv"

	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// KeyFileExtension is the file extension for the keys file. This file contains the keys for the data segment,
// and is used for performing garbage collection on the keymap. It can also be used to rebuild the keymap.
const KeyFileExtension = ".keys"

// KeyFileSwapExtension is the file extension for the keys swap file. This file is used to atomically
// update key files.
const KeyFileSwapExtension = KeyFileExtension + util.SwapFileExtension

// keyFile tracks the keys in a segment. It is used to do garbage collection on the keymap.
//
// This struct is NOT goroutine safe. It is unsafe to concurrently call write, flush, or seal on the same key file.
// It is not safe to read a key file until it is sealed. Once sealed, read only operations are goroutine safe.
type keyFile struct {
	// The logger for the key file.
	logger logging.Logger

	// The segment index.
	index uint32

	// Path data for the segment file.
	segmentPath *SegmentPath

	// The writer for the file. If the file is sealed, this value is nil.
	writer *bufio.Writer

	// The size of the key file in bytes.
	size uint64

	// The segment version. Determines serialization format.
	segmentVersion SegmentVersion

	// If true, then this key file is intended to replace another key file. It is written to a temporary
	// file, and then atomically renamed to the final file name.
	swap bool
}

// newKeyFile creates a new key file.
func createKeyFile(
	logger logging.Logger,
	index uint32,
	segmentPath *SegmentPath,
	swap bool,
) (*keyFile, error) {

	keys := &keyFile{
		logger:         logger,
		index:          index,
		segmentPath:    segmentPath,
		segmentVersion: ValueSizeSegmentVersion,
		swap:           swap,
	}

	filePath := keys.path()

	exists, _, err := util.ErrIfNotWritableFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("can not write to file: %w", err)
	}

	if exists {
		return nil, fmt.Errorf("key file %s already exists", filePath)
	}

	flags := os.O_RDWR | os.O_CREATE
	file, err := os.OpenFile(filePath, flags, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open key file: %w", err)
	}

	writer := bufio.NewWriter(file)
	keys.writer = writer

	return keys, nil
}

// loadKeyFile loads the key file from disk, looking in the given parent directories until it finds the file.
// If the file is not found, it returns an error.
func loadKeyFile(
	logger logging.Logger,
	index uint32,
	segmentPaths []*SegmentPath,
	segmentVersion SegmentVersion,
) (*keyFile, error) {

	keyFileName := fmt.Sprintf("%d%s", index, KeyFileExtension)
	keysPath, err := lookForFile(segmentPaths, keyFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to find key file: %w", err)
	}
	if keysPath == nil {
		return nil, fmt.Errorf("failed to find key file %s", keyFileName)
	}

	keys := &keyFile{
		logger:         logger,
		index:          index,
		segmentPath:    keysPath,
		segmentVersion: segmentVersion,
	}

	filePath := keys.path()

	exists, size, err := util.ErrIfNotWritableFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("can not write to file: %w", err)
	}

	if exists {
		keys.size = uint64(size)
	}

	if !exists {
		return nil, fmt.Errorf("key file %s does not exist", filePath)
	}

	return keys, nil
}

// Size returns the size of the key file in bytes.
func (k *keyFile) Size() uint64 {
	return k.size
}

// name returns the name of the key file.
func (k *keyFile) name() string {
	extension := KeyFileExtension
	if k.swap {
		extension = KeyFileSwapExtension
	}

	return fmt.Sprintf("%d%s", k.index, extension)
}

// path returns the full path to the key file.
func (k *keyFile) path() string {
	return path.Join(k.segmentPath.SegmentDirectory(), k.name())
}

// atomicSwap atomically replaces the key file, replacing the old one.
func (k *keyFile) atomicSwap(sync bool) error {
	if !k.swap {
		return fmt.Errorf("key file is not a swap file")
	}

	swapPath := k.path()
	k.swap = false
	newPath := k.path()

	err := util.AtomicRename(swapPath, newPath, sync)
	if err != nil {
		return fmt.Errorf("failed to atomically swap key file %s with %s: %w", swapPath, newPath, err)
	}

	return nil
}

// write writes a key to the key file.
func (k *keyFile) write(scopedKey *types.ScopedKey) error {
	if k.writer == nil {
		return fmt.Errorf("key file is sealed")
	}

	// Write the length of the key.
	err := binary.Write(k.writer, binary.BigEndian, uint32(len(scopedKey.Key)))
	if err != nil {
		return fmt.Errorf("failed to write key length to key file: %w", err)
	}

	// Write the key itself.
	_, err = k.writer.Write(scopedKey.Key)
	if err != nil {
		return fmt.Errorf("failed to write key to key file: %w", err)
	}

	// Write the address.
	err = binary.Write(k.writer, binary.BigEndian, scopedKey.Address)
	if err != nil {
		return fmt.Errorf("failed to write address to key file: %w", err)
	}

	// Write the size of the value.
	err = binary.Write(k.writer, binary.BigEndian, scopedKey.ValueSize)
	if err != nil {
		return fmt.Errorf("failed to write value size to key file: %w", err)
	}

	k.size += uint64(
		4 /* uint32 size of key */ +
			len(scopedKey.Key) +
			8 /* uint64 address */ +
			4 /* uint32 size of value */)

	return nil
}

// getKeyFileIndex returns the index of the key file from the file name. Key file names have the form "X.keys",
// where X is the segment index.
func getKeyFileIndex(fileName string) (uint32, error) {
	baseName := path.Base(fileName)
	indexString := baseName[:len(baseName)-len(KeyFileExtension)]
	index, err := strconv.Atoi(indexString)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index from file name %s: %w", fileName, err)
	}

	return uint32(index), nil
}

// flush flushes the key file to disk.
func (k *keyFile) flush() error {
	if k.writer == nil {
		return fmt.Errorf("key file is sealed")
	}

	return k.writer.Flush()
}

// seal seals the key file, preventing further writes.
func (k *keyFile) seal() error {
	if k.writer == nil {
		return fmt.Errorf("key file is already sealed")
	}

	err := k.flush()
	if err != nil {
		return fmt.Errorf("failed to flush key file: %w", err)
	}
	k.writer = nil

	return nil
}

// readKeys reads all keys from the key file. This method returns an error if the key file is not sealed.
// If there are keys that were only partially written (i.e. keys being written when the process crashed), then
// those keys may not be returned. If a key is returned, it is guaranteed to be "whole" (i.e. a partial key will
// never be returned).
func (k *keyFile) readKeys() ([]*types.ScopedKey, error) {
	if !k.isSealed() {
		return nil, fmt.Errorf("key file is not sealed")
	}

	file, err := os.Open(k.path())
	if err != nil {
		return nil, fmt.Errorf("failed to open key file: %w", err)
	}
	defer func() {
		err = file.Close()
		if err != nil {
			k.logger.Errorf("failed to close key file: %v", err)
		}
	}()

	// Key files are small as long as key length is sane. Safe to read the whole file into memory.
	keyBytes, err := os.ReadFile(k.path())
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	keys := make([]*types.ScopedKey, 0)

	index := 0
	for {
		// We need at least 4 bytes to read the length of the key.
		if index+4 > len(keyBytes) { //nolint:staticcheck // QF1006
			// There are fewer than 4 bytes left in the file.
			break
		}
		keyLength := int(binary.BigEndian.Uint32(keyBytes[index : index+4]))
		index += 4

		if k.segmentVersion < ValueSizeSegmentVersion {
			// We need to read the key, as well as the 8 byte address.
			if index+keyLength+8 > len(keyBytes) {
				// There are insufficient bytes left in the file to read the key and address.
				break
			}
		} else {
			// We need to read the key, as well as the 8 byte address and 4 byte value size.
			if index+keyLength+12 > len(keyBytes) {
				// There are insufficient bytes left in the file to read the key, address, and value size.
				break
			}
		}

		key := keyBytes[index : index+keyLength]
		index += keyLength

		address := types.Address(binary.BigEndian.Uint64(keyBytes[index : index+8]))
		index += 8

		var valueSize uint32
		if k.segmentVersion >= ValueSizeSegmentVersion {
			valueSize = binary.BigEndian.Uint32(keyBytes[index : index+4])
			index += 4
		}

		keys = append(keys, &types.ScopedKey{
			Key:       key,
			Address:   address,
			ValueSize: valueSize,
		})
	}

	if index != len(keyBytes) {
		// This can happen if there is a crash while we are writing to the key file.
		// Recoverable, but best to note the event in the logs.
		k.logger.Warnf("key file %s has %d partial bytes", k.path(), len(keyBytes)-index)
	}

	return keys, nil
}

// snapshot creates a hard link to the file in the snapshot directory, and a soft link to the hard linked file in the
// soft link directory. Requires that the file is sealed and that snapshotting is enabled.
func (k *keyFile) snapshot() error {
	if !k.isSealed() {
		return fmt.Errorf("file %s is not sealed, cannot take Snapshot", k.path())
	}

	err := k.segmentPath.Snapshot(k.name())
	if err != nil {
		return fmt.Errorf("failed to create Snapshot: %w", err)
	}

	return nil
}

// delete deletes the key file. If this key_file is a snapshot file (i.e. it is backed by a symlink), this method will
// also delete the file pointed to by the symlink.
func (k *keyFile) delete() error {
	if !k.isSealed() {
		return fmt.Errorf("key file %s is not sealed, cannot delete", k.path())
	}

	err := util.DeepDelete(k.path())
	if err != nil {
		return fmt.Errorf("failed to delete key file %s: %w", k.path(), err)
	}

	return nil
}

// isSealed returns true if the key file is sealed, and false otherwise.
func (k *keyFile) isSealed() bool {
	return k.writer == nil
}
