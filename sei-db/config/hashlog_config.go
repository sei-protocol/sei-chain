package config

import "github.com/sei-protocol/sei-chain/sei-db/common/unit"

// HashLoggerConfig configures the per-block hash logger: a debugging/forensics tool that records a CSV
// of named block hashes (memIAVL module/root hashes, flatKV DB/root hashes, the app hash, the block
// hash, and the changeset hash) so that block-hash computation can be studied and compared across nodes.
type HashLoggerConfig struct {
	// Enable turns on per-block hash logging. Defaults to true.
	Enable bool `mapstructure:"enable"`

	// Directory is where hash log files are written. If empty, defaults to a "hashlog" subdirectory of
	// the state-commit store directory.
	Directory string `mapstructure:"directory"`

	// BlocksToRetain is the number of most-recent blocks to keep on disk.
	BlocksToRetain uint `mapstructure:"blocks-to-retain"`

	// TargetFileSize is the size in bytes a log file may reach before it is sealed and rotated.
	TargetFileSize uint `mapstructure:"target-file-size"`

	// MaxDiskSize is a backstop cap (bytes) on the total size of sealed log files.
	MaxDiskSize uint `mapstructure:"max-disk-size"`

	// Version is the software version embedded in hash log file names. It is populated by the app layer
	// (from the node's build version), not parsed from config.
	Version string `mapstructure:"-"`
}

// DefaultHashLoggerConfig returns the default HashLoggerConfig. The retention defaults mirror
// hashlog.DefaultHashLoggerConfig.
func DefaultHashLoggerConfig() HashLoggerConfig {
	return HashLoggerConfig{
		Enable:         true,
		BlocksToRetain: 1_000_000,
		TargetFileSize: unit.MB,
		MaxDiskSize:    unit.GB,
	}
}
