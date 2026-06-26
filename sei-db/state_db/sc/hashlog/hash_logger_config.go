package hashlog

import (
	"fmt"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// ChangesetHashType is the reserved hash type under which the logger records the changeset hash it computes itself from
// each block's change set (see HashLogger.ReportChangeset). It is owned by the logger, not the caller: the name is
// fixed (not configurable) so every hash log file uses the same changeset column name. It must never appear in
// HashLoggerConfig.HashTypes — Validate rejects a collision — and ReportHash rejects it, so a caller can never
// clobber or race the logger-computed changeset column.
const ChangesetHashType = "changeset"

// Hash type names are written verbatim into CSV headers and must not collide with the "," field
// separator or any other structural character. We restrict them to a small, safe allowlist. "/" is
// permitted so callers can namespace columns hierarchically (e.g. "memIAVL/mod/bank", "flatKV/root");
// it is CSV-safe and hash type names never appear in file names (only the sanitized version does).
var legalHashTypeRegex = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// Configuration for a HashLogger.
type HashLoggerConfig struct {
	// The location where the HashLogger writes its output.
	Path string

	// The software version currently running on this node. Sanitized at construction time (any
	// character outside [A-Za-z0-9._] is replaced with "_") so that it can be embedded in file names.
	Version string

	// The ordered set of caller-reported hash types this logger records. Each type becomes a column in the
	// CSV output, in this order, and a block is only written once a hash has been reported for every type.
	// This must not include the reserved ChangesetHashType: the changeset column is owned and computed by the logger,
	// not supplied via ReportHash.
	HashTypes []string

	// When true, changeset hashing is disabled entirely: no hasher thread is started, ReportChangeset becomes a no-op,
	// and no changeset column is recorded or awaited for block completion. To instead skip changeset hashing for an
	// individual block while changeset hashing is enabled, pass a nil change set to ReportChangeset.
	DisableChangesetHashing bool

	// The size of the channel for sending work to the hasher thread.
	HashBufferSize uint

	// The size of the channel for sending work to the writer thread.
	WriteBufferSize uint

	// The size of the channel for sending notifications to the control loop.
	ControlBufferSize uint

	// The maximum number of blocks buffered in the control loop awaiting completion. When this is exceeded,
	// the oldest buffered block is written to disk even if incomplete, unless it is still awaiting an
	// in-flight changeset hash (in which case the buffer is allowed to exceed this bound until the changeset arrives).
	// This bounds memory if a registered hash type is never reported for some block.
	MaxBufferedBlocks uint

	// The number of HashLog entries to retain on disk. Zero disables block-count retention (the disk-size
	// cap is then the only bound).
	BlocksToRetain uint

	// The size log files are allowed to get before we close one and open another. Must be greater than 0.
	TargetFileSize uint

	// A backstop against runaway disk growth. When the total size of sealed log files exceeds this
	// value, the oldest sealed files are deleted until it no longer does, even if that means retaining
	// fewer than BlocksToRetain blocks. Zero disables the disk-size cap (block-count retention is then the
	// only bound).
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
	// MaxDiskSize == 0 is allowed: it disables the disk-size cap (block-count retention is then the only
	// bound; if both are disabled the logger grows without bound, which is a deliberate operator choice).
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
		// ChangesetHashType is reserved for the logger-computed changeset column, so it may never be a caller-reported
		// type. This holds even when changeset hashing is disabled: the name stays reserved so a config can never
		// silently mean different columns depending on the flag.
		if hashType == ChangesetHashType {
			return fmt.Errorf("hash type %q is reserved for the logger-computed changeset column", hashType)
		}
		if _, ok := seen[hashType]; ok {
			return fmt.Errorf("duplicate hash type %q", hashType)
		}
		seen[hashType] = struct{}{}
	}

	// At least one column must be recorded: the changeset column (when enabled) or a caller-reported type.
	if c.DisableChangesetHashing && len(c.HashTypes) == 0 {
		return fmt.Errorf("at least one hash type is required")
	}

	return nil
}
