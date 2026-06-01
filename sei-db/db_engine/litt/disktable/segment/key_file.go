package segment

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
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
	logger *slog.Logger

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
	logger *slog.Logger,
	index uint32,
	segmentPath *SegmentPath,
	swap bool,
) (*keyFile, error) {

	keys := &keyFile{
		logger:         logger,
		index:          index,
		segmentPath:    segmentPath,
		segmentVersion: LatestSegmentVersion,
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
	file, err := os.OpenFile(filePath, flags, 0600) //nolint:gosec // path validated by segment manager
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
	logger *slog.Logger,
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
		keys.size = uint64(size) //nolint:gosec // file size is non-negative
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

// KeyRecordHeaderSize is the on-disk size of the fixed-width portion of a key-file record that
// precedes the variable-length key bytes: one byte of KeyKind followed by a uint16 key-length prefix.
const KeyRecordHeaderSize = 3

// MaxKeyLength is the maximum permitted length of a key in bytes. The key file stores the key
// length as a uint16, which is more than enough headroom for any realistic key.
const MaxKeyLength = 1<<16 - 1

// write writes a key to the key file.
func (k *keyFile) write(scopedKey *types.ScopedKey) error {
	if k.writer == nil {
		return fmt.Errorf("key file is sealed")
	}

	if len(scopedKey.Key) > MaxKeyLength {
		return fmt.Errorf("key length %d exceeds maximum of %d", len(scopedKey.Key), MaxKeyLength)
	}

	// Write the kind byte (1 B).
	err := k.writer.WriteByte(byte(scopedKey.Kind))
	if err != nil {
		return fmt.Errorf("failed to write kind to key file: %w", err)
	}

	// Write the length of the key (2 B, big-endian).
	err = binary.Write(k.writer, binary.BigEndian, uint16(len(scopedKey.Key))) //nolint:gosec // bounded above
	if err != nil {
		return fmt.Errorf("failed to write key length to key file: %w", err)
	}

	// Write the key itself.
	_, err = k.writer.Write(scopedKey.Key)
	if err != nil {
		return fmt.Errorf("failed to write key to key file: %w", err)
	}

	// Write the serialized address (which includes the shard ID and value size).
	_, err = k.writer.Write(scopedKey.Address.Serialize())
	if err != nil {
		return fmt.Errorf("failed to write address to key file: %w", err)
	}

	k.size += uint64( //nolint:gosec // sizes are non-negative
		KeyRecordHeaderSize +
			len(scopedKey.Key) +
			types.AddressSerializedSize)

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

	return uint32(index), nil //nolint:gosec // segment index fits uint32
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
			k.logger.Error("failed to close key file", "error", err)
		}
	}()

	// Key files are small as long as key length is sane. Safe to read the whole file into memory.
	keyBytes, err := os.ReadFile(k.path())
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	keys := make([]*types.ScopedKey, 0)

	index := 0
	// We need the fixed-width header (kind + uint16 key length) before we can decide whether the
	// next record fits.
	for index+KeyRecordHeaderSize <= len(keyBytes) {
		kind := types.KeyKind(keyBytes[index])
		index++
		keyLength := int(binary.BigEndian.Uint16(keyBytes[index : index+2]))
		index += 2

		// We need to read the key, as well as the serialized address (which embeds the shard ID and value size).
		if index+keyLength+types.AddressSerializedSize > len(keyBytes) {
			// There are insufficient bytes left in the file to read the key and address.
			break
		}

		key := keyBytes[index : index+keyLength]
		index += keyLength

		address, err := types.DeserializeAddress(keyBytes[index : index+types.AddressSerializedSize])
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize address: %w", err)
		}
		index += types.AddressSerializedSize

		keys = append(keys, &types.ScopedKey{
			Key:     key,
			Address: address,
			Kind:    kind,
		})
	}

	if index != len(keyBytes) {
		// This can happen if there is a crash while we are writing to the key file.
		// Recoverable, but best to note the event in the logs.
		k.logger.Warn("key file has partial bytes",
			"path", k.path(),
			"bytes", len(keyBytes)-index,
		)
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
