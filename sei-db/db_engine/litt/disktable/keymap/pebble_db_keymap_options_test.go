package keymap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKeymapPebbleOptions guards the keymap's pebble tuning: the change is
// invisible at the behavior level (the other tests pass with stock options
// too), so without this a revert to stock options would ship silently.
func TestKeymapPebbleOptions(t *testing.T) {
	opts := keymapPebbleOptions()

	require.Equal(t, uint64(64<<20), opts.MemTableSize, "memtable must stay sized for high key rates")

	require.NotNil(t, opts.CompactionConcurrencyRange)
	_, upper := opts.CompactionConcurrencyRange()
	require.Equal(t, 8, upper, "compaction must be allowed to scale out")

	for i := range opts.Levels {
		require.NotNil(t, opts.Levels[i].FilterPolicy, "level %d missing bloom filter for point-lookup reads", i)
	}
}
