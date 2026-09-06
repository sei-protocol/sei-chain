package statestub

import (
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"
)

// TestConfigureLevelsKeepsTargetFileSizesGrowing pins the hazard in supplying levels at all.
//
// Pebble derives target file sizes by extrapolating past the end of the Levels slice, doubling once per
// level. Supplying every level ends that extrapolation, so without setting the sizes by hand each level
// would sit at pebble's two megabyte default and the bottom level would hold sixty-four times as many
// files as it does now.
func TestConfigureLevelsKeepsTargetFileSizesGrowing(t *testing.T) {
	options := &pebble.Options{}
	configureLevels(options)
	options.EnsureDefaults()

	expected := int64(pebbleBaseTargetFileSize)
	for level := 0; level < pebbleLevelCount; level++ {
		require.Equal(t, expected, options.Level(level).TargetFileSize,
			"level %d's target file size does not match what pebble would have derived", level)
		expected *= 2
	}

	// A default configuration is what the derived sizes are being held against, so the two have to agree.
	derived := &pebble.Options{}
	derived.EnsureDefaults()
	for level := 0; level < pebbleLevelCount; level++ {
		require.Equal(t, derived.Level(level).TargetFileSize, options.Level(level).TargetFileSize,
			"level %d diverges from the sizing pebble derives on its own", level)
	}
}

// TestConfigureLevelsFiltersEveryLevelButTheBottom checks which levels carry a filter.
//
// Pebble does not read a bottom level filter for a point lookup, so one written there is paid for on
// every compaction and never consulted.
func TestConfigureLevelsFiltersEveryLevelButTheBottom(t *testing.T) {
	options := &pebble.Options{}
	configureLevels(options)
	options.EnsureDefaults()

	for level := 0; level < pebbleLevelCount; level++ {
		policy := options.Level(level).FilterPolicy
		if level == pebbleBottomLevel {
			require.Nil(t, policy, "the bottom level should carry no filter")
			continue
		}
		require.NotNil(t, policy, "level %d should carry a filter", level)
		require.Equal(t, "rocksdb.BuiltinBloomFilter", policy.Name(),
			"the name is what a reader matches a table's filter against")
	}

	// The policy has to reach the map a reader resolves names through, which EnsureDefaults populates.
	require.Contains(t, options.Filters, "rocksdb.BuiltinBloomFilter")
}
