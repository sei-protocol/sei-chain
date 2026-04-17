package composite

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// The lifecycle status of a migration.
type migrationStatus int

const (
	// Migration has not yet started. All keys are considered unmigrated.
	migrationNotStarted migrationStatus = iota
	// Migration is in progress. Some keys are considered migrated, some are considered unmigrated.
	migrationInProgress migrationStatus = iota
	// Migration is complete. All keys are considered migrated.
	migrationComplete migrationStatus = iota
)

//nolint:godot // diagram
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
	status migrationStatus
	// The module name of the highest migrated key.
	moduleName string
	// The highest migrated key.
	key []byte
}

var (
	// A boundary for a migration that has not yet started. No key is considered migrated.
	MigrationBoundaryNotStarted = MigrationBoundary{
		status: migrationNotStarted,
	}
	// A boundary for a migration that has completed. All keys are considered migrated.
	MigrationBoundaryComplete = MigrationBoundary{
		status: migrationComplete,
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
		status:     migrationInProgress,
		moduleName: moduleName,
		key:        key,
	}
}

// Checks to see if a key has been migrated yet. Compares key against boundary, returning true if key is to the left
// (or equal to) the boundary. Returns false if the key is to the right of the boundary.
func (mb *MigrationBoundary) IsMigrated(moduleName string, key []byte) bool {
	switch mb.status {
	case migrationNotStarted:
		return false
	case migrationInProgress:
		if mb.moduleName == moduleName {
			return bytes.Compare(key, mb.key) <= 0
		} else {
			return strings.Compare(moduleName, mb.moduleName) <= 0
		}
	case migrationComplete:
		return true
	default:
		panic(fmt.Sprintf("invalid migration status: %d", mb.status))
	}
}

// Serialize encodes the boundary as a byte slice.
// For notStarted/complete: [status byte]
// For inProgress: [status byte] [2-byte BE moduleName length] [moduleName] [key]
func (mb *MigrationBoundary) Serialize() []byte {
	switch mb.status {
	case migrationNotStarted, migrationComplete:
		return []byte{byte(mb.status)}
	case migrationInProgress:
		nameBytes := []byte(mb.moduleName)
		buf := make([]byte, 1+2+len(nameBytes)+len(mb.key))
		buf[0] = byte(mb.status)
		binary.BigEndian.PutUint16(buf[1:3], uint16(len(nameBytes)))
		copy(buf[3:3+len(nameBytes)], nameBytes)
		copy(buf[3+len(nameBytes):], mb.key)
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
	status := migrationStatus(data[0])
	switch status {
	case migrationNotStarted, migrationComplete:
		if len(data) != 1 {
			return MigrationBoundary{}, fmt.Errorf(
				"unexpected trailing data for status %d: %d extra bytes", status, len(data)-1)
		}
		return MigrationBoundary{status: status}, nil
	case migrationInProgress:
		if len(data) < 3 {
			return MigrationBoundary{}, fmt.Errorf(
				"in-progress data too short: need at least 3 bytes, got %d", len(data))
		}
		nameLen := int(binary.BigEndian.Uint16(data[1:3]))
		if len(data) < 3+nameLen {
			return MigrationBoundary{}, fmt.Errorf(
				"in-progress data too short for module name of length %d: got %d bytes", nameLen, len(data))
		}
		moduleName := string(data[3 : 3+nameLen])
		key := make([]byte, len(data)-3-nameLen)
		copy(key, data[3+nameLen:])
		return MigrationBoundary{status: status, moduleName: moduleName, key: key}, nil
	default:
		return MigrationBoundary{}, fmt.Errorf("invalid migration status: %d", status)
	}
}
