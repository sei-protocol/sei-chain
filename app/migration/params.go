// Package migration defines the module-agnostic governance parameters
// that control the state-commitment store's background data migration
// (currently memiavl->flatkv).
//
// These live outside any business module on purpose: the migration rate
// applies to whichever stores the SC router is migrating, so it is an
// app/storage-level concern rather than EVM-specific. The value is held in a
// dedicated x/params subspace and is editable via the standard
// ParameterChangeProposal gov flow. The app reads it once per block in
// BeginBlock and pushes it into the SC commit store.
package migration

import (
	"fmt"

	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

// SubspaceName is the x/params subspace that holds storage-migration controls.
const SubspaceName = "migration"

// KeyNumKeysToMigratePerBlock is the param key for the number of keys the
// in-flight SC migration advances per block.
var KeyNumKeysToMigratePerBlock = []byte("NumKeysToMigratePerBlock")

// DefaultNumKeysToMigratePerBlock leaves the migration paused. While it is 0
// (the default until a gov proposal raises it) the SC store does no migration
// work; this param is the sole source of the per-block rate.
const DefaultNumKeysToMigratePerBlock uint64 = 0

// ParamKeyTable returns the key table for the migration subspace.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable(
		paramtypes.NewParamSetPair(KeyNumKeysToMigratePerBlock, new(uint64), validateNumKeysToMigratePerBlock),
	)
}

// validateNumKeysToMigratePerBlock only type-checks the value; any uint64 is a
// valid (consensus-deterministic) rate, with 0 meaning "paused".
func validateNumKeysToMigratePerBlock(i interface{}) error {
	if _, ok := i.(uint64); !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}
