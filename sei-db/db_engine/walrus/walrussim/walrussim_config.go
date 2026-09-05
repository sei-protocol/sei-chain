package walrussim

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// The permitted shape of an instance name, which becomes a metric attribute value.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// KeyClass is a group of keys that share a write period.
//
// A key in this class is written once every Period blocks, so a query at a random height finds its last
// write a uniform [0, Period) blocks back. The mix of classes is therefore a direct dial on how far a
// backwards walk has to travel, which is the quantity this benchmark exists to measure.
type KeyClass struct {
	// How many distinct keys belong to this class.
	KeyCount uint64

	// How many blocks pass between successive writes of any one of this class's keys.
	Period uint64
}

// Config configures a walrussim run.
type Config struct {
	// Where the engine, the state stub, and its checkpoints are written.
	DataDir string

	// Whether to delete DataDir before starting, so a run begins from nothing.
	DeleteDataDirOnStartup bool

	// A short identifier for this run, used to label its metrics. Must match [a-zA-Z0-9_-]+.
	Name string

	// The seed the canned randomness is generated from. The same seed reproduces the same run.
	Seed int64

	// The size of the pre-generated buffer keys and values are cut from.
	CannedRandomSize int

	// The store name every generated changeset carries.
	StoreName string

	// The size in bytes of a generated key. Must be at least 20, the length of an EVM address.
	KeySize int

	// The size in bytes of a generated value.
	ValueSize int

	// The key classes the workload writes. Ids are handed out to classes in order.
	KeyClasses []KeyClass

	// How many ids beyond the classes belong to no class at all. Reads against these are the deepest walk
	// the engine can be asked for: every pod is probed, the floor is reached, and the answer is absent.
	NeverWrittenKeyCount uint64

	// One write in this many is a deletion rather than a value. 0 disables deletions.
	DeleteRate uint64

	// The first block the run writes.
	FirstBlock uint64

	// How many blocks to write before stopping. 0 runs until interrupted.
	BlockCount uint64

	// The size a pod's data section may reach before it is cut.
	TargetPodSize uint64

	// The rate each pod's bloom filter is sized for.
	BloomFalsePositiveRate float64

	// How many blocks below the newest one stay queryable.
	RetentionBlocks uint64

	// How many pods may be built at once. A build holds its blocks in memory, so this is bounded by memory
	// rather than by cores.
	PodBuildConcurrency int

	// Whether the state stub produces checkpoints for the engine to retain. With this off there is no floor
	// beneath the oldest pod, so retention never deletes anything and the write path is not slowed by a
	// second database — which is the mode to measure append throughput in.
	EnableSnapshots bool

	// How many blocks pass between checkpoints.
	SnapshotIntervalBlocks uint64

	// If greater than 0, throttle block production to this many blocks per second.
	MaxBlocksPerSecond float64

	// How many goroutines issue historical reads. 0 disables reading.
	ReadConcurrency int

	// Target total reads per second across all reader goroutines.
	ReadsPerSecond int

	// What fraction of reads target a key nothing ever writes, between 0 and 1.
	NeverWrittenReadFraction float64

	// Whether readers check each answer against the workload's own model. The check happens after the timer
	// stops, so it does not enter the measurement.
	VerifyReads bool

	// The address the Prometheus endpoint is served on. Empty disables it.
	MetricsAddr string

	// How many seconds pass between console progress lines.
	ConsoleUpdateIntervalSeconds float64
}

// DefaultConfig returns a configuration sized for a workstation rather than for the production target.
//
// The pod size in particular is far below the 4 GiB cap the format allows: a build holds its pod in memory,
// and several run at once.
func DefaultConfig() *Config {
	return &Config{
		DataDir:                "./walrussim-data",
		DeleteDataDirOnStartup: true,
		Name:                   "walrussim",
		Seed:                   1337,
		CannedRandomSize:       64 * unit.MB,
		StoreName:              "evm",
		KeySize:                53,
		ValueSize:              32,
		KeyClasses: []KeyClass{
			{KeyCount: 1_000, Period: 1},
			{KeyCount: 100_000, Period: 100},
			{KeyCount: 1_000_000, Period: 1_000},
			{KeyCount: 10_000_000, Period: 100_000},
		},
		NeverWrittenKeyCount:         1_000_000,
		DeleteRate:                   1_000,
		FirstBlock:                   1,
		BlockCount:                   0,
		TargetPodSize:                256 * unit.MB,
		BloomFalsePositiveRate:       0.01,
		RetentionBlocks:              1_000_000,
		PodBuildConcurrency:          2,
		EnableSnapshots:              true,
		SnapshotIntervalBlocks:       5_000,
		MaxBlocksPerSecond:           0,
		ReadConcurrency:              4,
		ReadsPerSecond:               2_000,
		NeverWrittenReadFraction:     0.25,
		VerifyReads:                  true,
		MetricsAddr:                  ":9090",
		ConsoleUpdateIntervalSeconds: 2,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if !nameRegex.MatchString(c.Name) {
		return fmt.Errorf("name %q is required and must match %s", c.Name, nameRegex.String())
	}
	if c.CannedRandomSize < c.ValueSize*2 || c.CannedRandomSize < c.KeySize*2 {
		return fmt.Errorf("canned random size %d is too small for %d byte keys and %d byte values",
			c.CannedRandomSize, c.KeySize, c.ValueSize)
	}
	if c.StoreName == "" {
		return fmt.Errorf("store name is required")
	}
	if c.KeySize < 20 {
		return fmt.Errorf("key size must be at least 20, got %d", c.KeySize)
	}
	if c.ValueSize < 0 {
		return fmt.Errorf("value size must not be negative, got %d", c.ValueSize)
	}
	if len(c.KeyClasses) == 0 {
		return fmt.Errorf("at least one key class is required")
	}
	for index, class := range c.KeyClasses {
		if class.KeyCount == 0 {
			return fmt.Errorf("key class %d has no keys", index)
		}
		if class.Period == 0 {
			return fmt.Errorf("key class %d has a period of 0", index)
		}
	}
	if c.NeverWrittenReadFraction < 0 || c.NeverWrittenReadFraction > 1 {
		return fmt.Errorf("never written read fraction must be in [0, 1], got %v", c.NeverWrittenReadFraction)
	}
	if c.NeverWrittenReadFraction > 0 && c.NeverWrittenKeyCount == 0 {
		return fmt.Errorf("never written reads were requested but no never written keys were configured")
	}
	if c.ReadConcurrency < 0 {
		return fmt.Errorf("read concurrency must not be negative, got %d", c.ReadConcurrency)
	}
	if c.ReadConcurrency > 0 && c.ReadsPerSecond <= 0 {
		return fmt.Errorf("read concurrency was requested but reads per second is %d", c.ReadsPerSecond)
	}
	if c.EnableSnapshots && c.SnapshotIntervalBlocks == 0 {
		return fmt.Errorf("snapshots were requested but the snapshot interval is 0")
	}
	if c.TargetPodSize == 0 {
		return fmt.Errorf("target pod size must be greater than 0")
	}
	if c.BloomFalsePositiveRate <= 0 || c.BloomFalsePositiveRate >= 1 {
		return fmt.Errorf("bloom false positive rate must be in (0, 1), got %v", c.BloomFalsePositiveRate)
	}
	if c.RetentionBlocks == 0 {
		return fmt.Errorf("retention blocks must be greater than 0")
	}
	if c.PodBuildConcurrency <= 0 {
		return fmt.Errorf("pod build concurrency must be greater than 0")
	}
	return nil
}

// LoadConfigFromFile reads a JSON configuration, rejecting any field it does not recognize so that a typo in
// a hand written config fails loudly instead of being silently ignored.
func LoadConfigFromFile(path string) (*Config, error) {
	file, err := os.Open(path) //nolint:gosec // the path is a command line argument
	if err != nil {
		return nil, fmt.Errorf("failed to open config %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	config := DefaultConfig()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", path, err)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return config, nil
}

// StringifiedConfig returns the configuration as indented JSON, which a run prints so that its output records
// exactly what produced it.
func (c *Config) StringifiedConfig() (string, error) {
	encoded, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to encode the config: %w", err)
	}
	return string(encoded), nil
}
