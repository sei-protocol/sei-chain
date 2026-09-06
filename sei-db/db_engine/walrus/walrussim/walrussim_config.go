package walrussim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus/statestub"
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

	// The engine under measurement.
	//
	// Its Path, Name, and StoreName are owned by the harness and filled in from the fields above, so a config
	// file that sets them is rejected rather than silently overruled. Everything else is the engine's own
	// default unless the file says otherwise.
	Walrus walrus.Config

	// The state stub that produces the snapshots the engine retains.
	//
	// Its Path, Name, and StoreName are owned by the harness on the same terms.
	StateStub statestub.Config

	// Whether the state stub produces checkpoints for the engine to retain. With this off there is no floor
	// beneath the oldest pod, so retention never deletes anything and the write path is not slowed by a
	// second database — which is the mode to measure append throughput in.
	EnableSnapshots bool

	// How many seconds pass between checkpoints.
	//
	// Cadence is wall clock rather than a block count because a block count only means something once you
	// know the block rate, and the block rate is what a run is measuring. Seconds tune the same whatever the
	// workload turns out to cost.
	SnapshotIntervalSeconds float64

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
		// Four classes contributing 2,500 writes each, for 10,000 key changes a block. Equal contribution
		// with periods an order of magnitude apart spreads how stale a key is over four orders of magnitude,
		// which is what a walk's depth is a function of.
		KeyClasses: []KeyClass{
			{KeyCount: 2_500, Period: 1},
			{KeyCount: 250_000, Period: 100},
			{KeyCount: 2_500_000, Period: 1_000},
			{KeyCount: 25_000_000, Period: 10_000},
		},
		NeverWrittenKeyCount:         1_000_000,
		DeleteRate:                   1_000,
		FirstBlock:                   1,
		BlockCount:                   0,
		Walrus:                       *walrus.DefaultConfig("", "", ""),
		StateStub:                    *statestub.DefaultConfig("", "", ""),
		EnableSnapshots:              true,
		SnapshotIntervalSeconds:      60,
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
	if c.EnableSnapshots && c.SnapshotIntervalSeconds <= 0 {
		return fmt.Errorf("snapshots were requested but the snapshot interval is %v seconds",
			c.SnapshotIntervalSeconds)
	}
	if err := requireHarnessOwned("Walrus", c.Walrus.Path, c.Walrus.Name, c.Walrus.StoreName); err != nil {
		return err
	}
	if err := requireHarnessOwned(
		"StateStub", c.StateStub.Path, c.StateStub.Name, c.StateStub.StoreName); err != nil {
		return err
	}

	// Validate what will actually be used, which is the embedded configuration with the harness's own fields
	// filled in. Validating it as written would fail on the very fields the harness is about to supply.
	if err := c.WalrusConfig().Validate(); err != nil {
		return fmt.Errorf("invalid engine config: %w", err)
	}
	if err := c.StateStubConfig().Validate(); err != nil {
		return fmt.Errorf("invalid state stub config: %w", err)
	}
	return nil
}

// WalrusConfig returns the engine configuration a run will use: what the file asked for, with the path and
// names the harness owns filled in.
func (c *Config) WalrusConfig() *walrus.Config {
	resolved := c.Walrus
	resolved.Path = filepath.Join(c.DataDir, engineDirName)
	resolved.Name = c.Name
	resolved.StoreName = c.StoreName
	return &resolved
}

// StateStubConfig returns the state stub configuration a run will use, on the same terms.
func (c *Config) StateStubConfig() *statestub.Config {
	resolved := c.StateStub
	resolved.Path = filepath.Join(c.DataDir, stubDirName)
	resolved.Name = c.Name
	resolved.StoreName = c.StoreName
	return &resolved
}

// requireHarnessOwned rejects an embedded configuration that sets a field the harness derives.
//
// Overwriting it silently would make a config file lie about what the run did, which is worse than refusing
// to start.
func requireHarnessOwned(section string, path string, name string, storeName string) error {
	for field, value := range map[string]string{"Path": path, "Name": name, "StoreName": storeName} {
		if value != "" {
			return fmt.Errorf("%s.%s is set by the harness; remove %q from the config",
				section, field, value)
		}
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
