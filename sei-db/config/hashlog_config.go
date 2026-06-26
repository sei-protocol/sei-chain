package config

import "github.com/sei-protocol/sei-chain/sei-db/common/unit"

// HashLoggerConfig configures the per-block hash logger: a debugging/forensics tool that records a CSV
// of named block hashes (memIAVL module/root hashes, flatKV DB/root hashes, the app hash, the block
// hash, and the changeset hash) so that block-hash computation can be studied and compared across nodes.
type HashLoggerConfig struct {
	// These fields are loaded by explicit flag reads in app.parseSCConfigs (keys: sc-hash-logger-*),
	// not via mapstructure, so they carry no mapstructure tags.

	// Enable turns on per-block hash logging. Defaults to true.
	Enable bool

	// Directory is where hash log files are written. If empty, defaults to a "hash.log" directory under
	// the state-commit store's data directory.
	Directory string

	// BlocksToRetain is the number of most-recent blocks to keep on disk. 0 disables block-count
	// retention (the disk-size cap is then the only bound).
	BlocksToRetain uint

	// TargetFileSize is the size in bytes a log file may reach before it is sealed and rotated. Must be > 0.
	TargetFileSize uint

	// MaxDiskSize is a backstop cap (bytes) on the total size of sealed log files. 0 disables the disk-size
	// cap (block-count retention is then the only bound).
	MaxDiskSize uint

	// Version is the software version embedded in hash log file names. It is populated by the app layer
	// (from the node's build version), not parsed from config.
	Version string
}

// DefaultHashLoggerConfig returns the default HashLoggerConfig. Retention is disk-driven: keep up to
// 16 GiB of sealed files (~7 weeks at tip), with block-count retention disabled.
func DefaultHashLoggerConfig() HashLoggerConfig {
	return HashLoggerConfig{
		Enable:         true,
		BlocksToRetain: 0, // disabled — retention is purely disk-driven
		TargetFileSize: 16 * unit.MB,
		MaxDiskSize:    16 * unit.GB,
	}
}
