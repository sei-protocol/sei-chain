package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/docker/go-units"
)

// BenchmarkConfig is a struct that holds the configuration for the benchmark.
type BenchmarkConfig struct {

	// Configuration for the LittDB instance.
	LittConfig *litt.Config

	// The location where the benchmark stores test metadata.
	MetadataDirectory string

	// The maximum target write throughput in MB/s.
	MaximumWriteThroughputMB float64

	// The maximum read throughput in MB/s.
	MaximumReadThroughputMB float64

	// The number of parallel write goroutines.
	WriterParallelism int

	// The number of parallel read goroutines.
	ReaderParallelism int

	// The size of the values in MB.
	ValueSizeMB float64

	// Data is written to the DB in batches and then flushed. This determines the size of those batches, in MB.
	BatchSizeMB float64

	// The frequency at which the benchmark does cohort garbage collection, in seconds
	CohortGCPeriodSeconds float64

	// The size of the write info channel. Controls the max number of keys to prepare for writing ahead of time.
	WriteInfoChanelSize uint64

	// The size of the read info channel. Controls the max number of keys to prepare for reading ahead of time.
	ReadInfoChanelSize uint64

	// The number of keys in a new cohort.
	CohortSize uint64

	// The time-to-live (TTL) for keys in the database, in hours.
	TTLHours float64

	// If data is within this many minutes of its expiration time, it will not be read.
	ReadSafetyMarginMinutes float64

	// A seed for the random number generator used to generate keys and values. When restarting the benchmark,
	// it's important to always use the same seed.
	Seed int64

	// The size of the pool of random data. Instead of generating random data for each key/value pair
	// (which is expensive), data from this pool is reused. When restarting the benchmark,
	// it's important to always use the same pool size.
	RandomPoolSize uint64

	// When the benchmark starts, it sleeps for a length of time. The average amount of time spent sleeping is equal to
	// this value, in seconds. The purpose of this sleeping to stagger the start of the workers so that they don't all
	// operate in lockstep.
	StartupSleepFactorSeconds float64

	// The frequency at which the benchmark logs metrics, in seconds. If zero, then metrics logging is disabled.
	MetricsLoggingPeriodSeconds float64

	// If true, the benchmark will panic and halt if there is a read failure.
	// There is currently a rare bug somewhere, I suspect in metadata tracking. The bug can cause
	// the benchmark to read a key that is no longer present in the database. Until that bug is fixed,
	// do not halt the benchmark on read failures by default.
	PanicOnReadFailure bool

	// If true, fsync cohort files to ensure atomicity. Can be set to false for unit tests that need to be fast.
	Fsync bool

	// If non-zero, then the benchmark will run for this many seconds and then stop. If zero,
	// the benchmark will run until it is manually stopped.
	TimeLimitSeconds float64
}

// DefaultBenchmarkConfig returns a default BenchmarkConfig.
func DefaultBenchmarkConfig() *BenchmarkConfig {

	littConfig := litt.DefaultConfigNoPaths()
	littConfig.LoggerConfig = common.DefaultConsoleLoggerConfig()
	littConfig.MetricsEnabled = true

	return &BenchmarkConfig{
		LittConfig:                  littConfig,
		MetadataDirectory:           "~/benchmark",
		MaximumWriteThroughputMB:    10,
		MaximumReadThroughputMB:     10,
		WriterParallelism:           4,
		ReaderParallelism:           32,
		ValueSizeMB:                 2.0,
		BatchSizeMB:                 32,
		CohortGCPeriodSeconds:       10.0,
		WriteInfoChanelSize:         1024,
		ReadInfoChanelSize:          1024,
		CohortSize:                  1024,
		TTLHours:                    1.0,
		ReadSafetyMarginMinutes:     5.0,
		Seed:                        1337,
		RandomPoolSize:              units.GiB,
		StartupSleepFactorSeconds:   0.5,
		MetricsLoggingPeriodSeconds: 60.0,
		PanicOnReadFailure:          false,
		TimeLimitSeconds:            0.0,
	}
}

// LoadConfig loads the benchmark configuration from the json file at the given path.
func LoadConfig(path string) (*BenchmarkConfig, error) {
	config := DefaultBenchmarkConfig()

	path, err := util.SanitizePath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to sanitize path: %w", err)
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Create a decoder that will return an error if there are unmatched fields
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()

	// Unmarshal JSON into config struct
	err = decoder.Decode(config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file: %w", err)
	}

	config.MetadataDirectory, err = util.SanitizePath(config.MetadataDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to sanitize metadata directory: %w", err)
	}

	return config, nil
}
