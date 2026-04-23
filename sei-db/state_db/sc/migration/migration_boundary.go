package migration

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

//nolint:godot
/*
                        <-- Lexographically ordered keys -->

      keys in this region migrated           keys in this region not migrated
    |--------------------------------------|--------------------------------------|
    ^                                      ^                                      ^
    |                                      |                                      |
  first key                       migration boundary                          last key
                                --> moves each block -->
*/

// headerSize is the size of the fixed-length prefix of an in-progress
// serialized boundary: 1 status byte + 4-byte big-endian module-name length.
const headerSize = 5

// Defines the boundary between migrated and unmigrated keys.
type MigrationBoundary struct {
	// The status of the migration.
	status MigrationStatus
	// The module name of the highest migrated key.
	moduleName string
	// The highest migrated key.
	key []byte
}

var (
	// A boundary for a migration that has not yet started. No key is considered migrated.
	MigrationBoundaryNotStarted = MigrationBoundary{
		status: MigrationNotStarted,
	}
	// A boundary for a migration that has completed. All keys are considered migrated.
	MigrationBoundaryComplete = MigrationBoundary{
		status: MigrationComplete,
	}
)

// Create a new migration boundary with the given key.
//
// The key slice is stored by reference; callers must not mutate it after
// this call.
func NewMigrationBoundary(
	// The module name of the highest migrated key.
	moduleName string,
	// The highest migrated key.
	key []byte,
) MigrationBoundary {
	return MigrationBoundary{
		status:     MigrationInProgress,
		moduleName: moduleName,
		key:        key,
	}
}

// Equals returns true if two boundaries represent the same migration state.
func (mb *MigrationBoundary) Equals(other MigrationBoundary) bool {
	return mb.status == other.status &&
		mb.moduleName == other.moduleName &&
		bytes.Equal(mb.key, other.key)
}

// Status returns the lifecycle status of the migration.
func (mb *MigrationBoundary) Status() MigrationStatus {
	return mb.status
}

// ModuleName returns the module name of the highest migrated key.
func (mb *MigrationBoundary) ModuleName() string {
	return mb.moduleName
}

// Key returns the highest migrated key. The returned slice aliases the
// boundary's internal state and must not be mutated.
func (mb *MigrationBoundary) Key() []byte {
	return mb.key
}

// String returns a human-readable representation of the boundary.
// For in-progress boundaries the key is hex-encoded so that arbitrary binary
// keys round-trip unambiguously.
//
// Format: "MigrationBoundary{status=<status>, module=<name>, key=<hex>}"
func (mb *MigrationBoundary) String() string {
	switch mb.status {
	case MigrationNotStarted, MigrationComplete:
		return fmt.Sprintf("MigrationBoundary{status=%s}", mb.status)
	default:
		return fmt.Sprintf("MigrationBoundary{status=%s, module=%s, key=%s}",
			mb.status, mb.moduleName, hex.EncodeToString(mb.key))
	}
}

// Checks to see if a key has been migrated yet. Compares key against boundary, returning true if key is to the left
// (or equal to) the boundary. Returns false if the key is to the right of the boundary.
func (mb *MigrationBoundary) IsMigrated(moduleName string, key []byte) bool {
	switch mb.status {
	case MigrationNotStarted:
		return false
	case MigrationInProgress:
		if mb.moduleName == moduleName {
			return bytes.Compare(key, mb.key) <= 0
		} else {
			return strings.Compare(moduleName, mb.moduleName) <= 0
		}
	case MigrationComplete:
		return true
	default:
		panic(fmt.Sprintf("invalid migration status: %d", mb.status))
	}
}

// Serialize encodes the boundary as a byte slice. The returned slice is
// freshly allocated and independent of the boundary.
//
// For notStarted/complete: [status byte]
// For inProgress: [status byte] [4-byte BE moduleName length] [moduleName] [key]
func (mb *MigrationBoundary) Serialize() []byte {
	switch mb.status {
	case MigrationNotStarted, MigrationComplete:
		return []byte{byte(mb.status)}
	case MigrationInProgress:
		nameBytes := []byte(mb.moduleName)
		buf := make([]byte, headerSize+len(nameBytes)+len(mb.key))
		buf[0] = byte(mb.status)
		binary.BigEndian.PutUint32(buf[1:headerSize], uint32(len(nameBytes))) //nolint:gosec
		copy(buf[headerSize:headerSize+len(nameBytes)], nameBytes)
		copy(buf[headerSize+len(nameBytes):], mb.key)
		return buf
	default:
		panic(fmt.Sprintf("invalid migration status: %d", mb.status))
	}
}

// DeserializeMigrationBoundary decodes a byte slice produced by Serialize.
// The returned boundary owns its key and is independent of the input slice.
func DeserializeMigrationBoundary(data []byte) (MigrationBoundary, error) {
	if len(data) == 0 {
		return MigrationBoundary{}, fmt.Errorf("empty migration boundary data")
	}
	status := MigrationStatus(data[0])
	switch status {
	case MigrationNotStarted, MigrationComplete:
		if len(data) != 1 {
			return MigrationBoundary{}, fmt.Errorf(
				"unexpected trailing data for status %d: %d extra bytes", status, len(data)-1)
		}
		return MigrationBoundary{status: status}, nil
	case MigrationInProgress:
		if len(data) < headerSize {
			return MigrationBoundary{}, fmt.Errorf(
				"in-progress data too short: need at least %d bytes, got %d", headerSize, len(data))
		}
		nameLen := int(binary.BigEndian.Uint32(data[1:headerSize]))
		if len(data) < headerSize+nameLen {
			return MigrationBoundary{}, fmt.Errorf(
				"in-progress data too short for module name of length %d: got %d bytes", nameLen, len(data))
		}
		moduleName := string(data[headerSize : headerSize+nameLen])
		key := make([]byte, len(data)-headerSize-nameLen)
		copy(key, data[headerSize+nameLen:])
		return MigrationBoundary{status: status, moduleName: moduleName, key: key}, nil
	default:
		return MigrationBoundary{}, fmt.Errorf("invalid migration status: %d", status)
	}
}
