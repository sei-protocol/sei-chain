package hashlog

import (
	"fmt"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// The default hash type under which ReportDiff's computed hash is recorded.
const defaultDiffHashType = "diff"

// DiffHashingDisabled is the value to assign to HashLoggerConfig.DiffHashType to disable diff hashing entirely:
// no hasher thread is started and ReportDiff becomes a no-op. To instead skip diff hashing for an individual
// block while diff hashing is enabled, pass a nil change set to ReportDiff.
const DiffHashingDisabled = ""

// Hash type names are written verbatim into CSV headers and must not collide with the "," field
// separator or any other structural character. We restrict them to a small, safe allowlist.
var legalHashTypeRegex = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Configuration for a HashLogger.
type HashLoggerConfig struct {
	// The location where the HashLogger writes its output.
	Path string

	// The software version currently running on this node. Sanitized at construction time (any
	// character outside [A-Za-z0-9._] is replaced with "_") so that it can be embedded in file names.
	Version string

	// The ordered set of hash types this logger records. Each type becomes a column in the CSV output,
	// in this order, and a block is only written once a hash has been reported for every type.
	HashTypes []string

	// The hash type under which ReportDiff's computed hash is recorded. Must be one of HashTypes, or
	// DiffHashingDisabled to disable diff hashing entirely (in which case ReportDiff is a no-op).
	DiffHashType string

	// The size of the channel for sending work to the hasher thread.
	HashBufferSize uint

	// The size of the channel for sending work to the writer thread.
	WriteBufferSize uint

	// The size of the channel for sending notifications to the control loop.
	ControlBufferSize uint

	// If we don't receive all information within this many blocks, flush to disk regardless.
	// The hash logger buffers data until it has all info for a block, but we shouldn't buffer
	// it forever.
	MaxBlockDelay uint

	// The number of HashLog entries to retain on disk.
	BlocksToRetain uint

	// The size log files are allowed to get before we close one and open another.
	TargetFileSize uint

	// A backstop against runaway disk growth. When the total size of sealed log files exceeds this
	// value, the oldest sealed files are deleted until it no longer does, even if that means retaining
	// fewer than BlocksToRetain blocks.
	MaxDiskSize uint
}

// DefaultHashLoggerConfig returns a default configuration for a HashLogger.
func DefaultHashLoggerConfig(path string, version string) *HashLoggerConfig {
	return &HashLoggerConfig{
		Path:              path,
		Version:           version,
		HashTypes:         []string{defaultDiffHashType, "flatKV", "memIAVL", "root"},
		DiffHashType:      defaultDiffHashType,
		HashBufferSize:    16,
		WriteBufferSize:   16,
		ControlBufferSize: 16,
		MaxBlockDelay:     16,
		BlocksToRetain:    1_000_000,
		TargetFileSize:    unit.MB,
		MaxDiskSize:       unit.GB,
	}
}

// Validate checks that the HashLoggerConfig is valid.
func (c *HashLoggerConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}
	if c.MaxDiskSize == 0 {
		return fmt.Errorf("max disk size must be greater than 0")
	}
	if len(c.HashTypes) == 0 {
		return fmt.Errorf("at least one hash type is required")
	}

	seen := make(map[string]struct{}, len(c.HashTypes))
	for _, hashType := range c.HashTypes {
		if !legalHashTypeRegex.MatchString(hashType) {
			return fmt.Errorf("hash type %q contains illegal characters (must match %s)",
				hashType, legalHashTypeRegex.String())
		}
		if _, ok := seen[hashType]; ok {
			return fmt.Errorf("duplicate hash type %q", hashType)
		}
		seen[hashType] = struct{}{}
	}

	if c.DiffHashType != DiffHashingDisabled {
		if _, ok := seen[c.DiffHashType]; !ok {
			return fmt.Errorf("diff hash type %q is not one of the configured hash types", c.DiffHashType)
		}
	}

	return nil
}
