package migration

import (
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
	// Any uint64 is valid, including 0 (paused) and large values.
	for _, v := range []uint64{0, 1, 1024, 1 << 40} {
		require.NoError(t, validateNumKeysToMigratePerBlock(v), "uint64 %d should be valid", v)
	}

	// Wrong types are rejected.
	for _, v := range []interface{}{int(1), int64(1), "1", float64(1), nil} {
		require.Error(t, validateNumKeysToMigratePerBlock(v), "value %v (%T) should be rejected", v, v)
	}
}
