package migration

import (
	"math"
	"testing"

	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultLeavesMigrationPaused(t *testing.T) {
	require.Equal(t, uint64(0), DefaultNumKeysToMigratePerBlock,
		"default must be 0 so the migration stays paused until governance raises it")
}

func TestParamKeyTableRegistersKey(t *testing.T) {
	table := ParamKeyTable()

	// Re-registering the same key must panic with "duplicate parameter key",
	// which proves NumKeysToMigratePerBlock is already in the returned table.
	require.PanicsWithValue(t, "duplicate parameter key", func() {
		table.RegisterType(paramtypes.NewParamSetPair(
			KeyNumKeysToMigratePerBlock, new(uint64), validateNumKeysToMigratePerBlock))
	})
}

func TestValidateNumKeysToMigratePerBlock(t *testing.T) {
	// Any uint64 in [0, max] is valid, including 0 (paused) and the boundary.
	for _, v := range []uint64{0, 1, 1024, MaxNumKeysToMigratePerBlock} {
		require.NoError(t, validateNumKeysToMigratePerBlock(v), "uint64 %d should be valid", v)
	}

	// Values above the cap are rejected so they can never reach chain state
	// and OOM/panic the migration iterator's preallocation.
	for _, v := range []uint64{MaxNumKeysToMigratePerBlock + 1, 1 << 40, math.MaxUint64} {
		require.Error(t, validateNumKeysToMigratePerBlock(v), "uint64 %d should be rejected as too large", v)
	}

	// Wrong types are rejected.
	for _, v := range []interface{}{int(1), int64(1), "1", float64(1), nil} {
		require.Error(t, validateNumKeysToMigratePerBlock(v), "value %v (%T) should be rejected", v, v)
	}
}
