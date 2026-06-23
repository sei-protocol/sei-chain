package hashlog

import (
	"fmt"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// DiffHashType is the reserved hash type under which the logger records the diff hash it computes itself from
// each block's change set (see HashLogger.ReportDiff). It is owned by the logger, not the caller: the name is
// fixed (not configurable) so every hash log file uses the same diff column name. It must never appear in
// HashLoggerConfig.HashTypes — Validate rejects a collision — and ReportHash rejects it, so a caller can never
// clobber or race the logger-computed diff column.
const DiffHashType = "diff"

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

	// The ordered set of caller-reported hash types this logger records. Each type becomes a column in the
	// CSV output, in this order, and a block is only written once a hash has been reported for every type.
	// This must not include the reserved DiffHashType: the diff column is owned and computed by the logger,
	// not supplied via ReportHash.
	HashTypes []string

	// When true, diff hashing is disabled entirely: no hasher thread is started, ReportDiff becomes a no-op,
	// and no diff column is recorded or awaited for block completion. To instead skip diff hashing for an
	// individual block while diff hashing is enabled, pass a nil change set to ReportDiff.
	DisableDiffHashing bool

	// The size of the channel for sending work to the hasher thread.
	HashBufferSize uint

	// The size of the channel for sending work to the writer thread.
	WriteBufferSize uint

	// The size of the channel for sending notifications to the control loop.
	ControlBufferSize uint

	// The maximum number of blocks buffered in the control loop awaiting completion. When this is exceeded,
	// the oldest buffered block is written to disk even if incomplete, unless it is still awaiting an
	// in-flight diff hash (in which case the buffer is allowed to exceed this bound until the diff arrives).
	// This bounds memory if a registered hash type is never reported for some block.
	MaxBufferedBlocks uint

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
		HashTypes:         []string{},
		HashBufferSize:    16,
		WriteBufferSize:   16,
		ControlBufferSize: 16,
		MaxBufferedBlocks: 1024,
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
	if c.MaxBufferedBlocks == 0 {
		return fmt.Errorf("max buffered blocks must be greater than 0")
	}
	if c.TargetFileSize == 0 {
		// A zero target would seal and rotate a fresh file after every single block.
		return fmt.Errorf("target file size must be greater than 0")
	}

	seen := make(map[string]struct{}, len(c.HashTypes))
	for _, hashType := range c.HashTypes {
		if !legalHashTypeRegex.MatchString(hashType) {
			return fmt.Errorf("hash type %q contains illegal characters (must match %s)",
				hashType, legalHashTypeRegex.String())
		}
		// DiffHashType is reserved for the logger-computed diff column, so it may never be a caller-reported
		// type. This holds even when diff hashing is disabled: the name stays reserved so a config can never
		// silently mean different columns depending on the flag.
		if hashType == DiffHashType {
			return fmt.Errorf("hash type %q is reserved for the logger-computed diff column", hashType)
		}
		if _, ok := seen[hashType]; ok {
			return fmt.Errorf("duplicate hash type %q", hashType)
		}
		seen[hashType] = struct{}{}
	}

	// At least one column must be recorded: the diff column (when enabled) or a caller-reported type.
	if c.DisableDiffHashing && len(c.HashTypes) == 0 {
		return fmt.Errorf("at least one hash type is required")
	}

	return nil
}
