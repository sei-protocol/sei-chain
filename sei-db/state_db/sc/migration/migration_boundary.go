package migration

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// MigrationStatus is the lifecycle status of a migration.
type MigrationStatus int

const (
	// MigrationNotStarted means the migration has not yet started. All keys are considered unmigrated.
	MigrationNotStarted MigrationStatus = iota
	// MigrationInProgress means the migration is in progress. Some keys are migrated, some are not.
	MigrationInProgress MigrationStatus = iota
	// MigrationComplete means the migration is complete. All keys are considered migrated.
	MigrationComplete MigrationStatus = iota
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

// Key returns the highest migrated key.
func (mb *MigrationBoundary) Key() []byte {
	return mb.key
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

// Serialize encodes the boundary as a byte slice.
// For notStarted/complete: [status byte]
// For inProgress: [status byte] [4-byte BE moduleName length] [moduleName] [key]
func (mb *MigrationBoundary) Serialize() []byte {
	switch mb.status {
	case MigrationNotStarted, MigrationComplete:
		return []byte{byte(mb.status)}
	case MigrationInProgress:
		nameBytes := []byte(mb.moduleName)
		buf := make([]byte, 1+4+len(nameBytes)+len(mb.key))
		buf[0] = byte(mb.status)
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(nameBytes))) //nolint:gosec // module names are short strings
		copy(buf[5:5+len(nameBytes)], nameBytes)
		copy(buf[5+len(nameBytes):], mb.key)
		return buf
	default:
		panic(fmt.Sprintf("invalid migration status: %d", mb.status))
	}
}

// DeserializeMigrationBoundary decodes a byte slice produced by Serialize.
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
		if len(data) < 5 {
			return MigrationBoundary{}, fmt.Errorf(
				"in-progress data too short: need at least 5 bytes, got %d", len(data))
		}
		nameLen := int(binary.BigEndian.Uint32(data[1:5]))
		if len(data) < 5+nameLen {
			return MigrationBoundary{}, fmt.Errorf(
				"in-progress data too short for module name of length %d: got %d bytes", nameLen, len(data))
		}
		moduleName := string(data[5 : 5+nameLen])
		key := make([]byte, len(data)-5-nameLen)
		copy(key, data[5+nameLen:])
		return MigrationBoundary{status: status, moduleName: moduleName, key: key}, nil
	default:
		return MigrationBoundary{}, fmt.Errorf("invalid migration status: %d", status)
	}
}
