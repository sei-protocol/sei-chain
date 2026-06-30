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
//
// Caveat: this subspace has no owning AppModule, so the x/params module's
// ExportGenesis (which only emits Fees/CosmosGas params) does not serialize
// NumKeysToMigratePerBlock. A `seid export` taken mid-migration therefore
// omits the rate, and a chain bootstrapped from that genesis re-seeds the
// default (0, paused) on the first BeginBlock. This is not consensus-fatal —
// re-issue the ParameterChangeProposal on the new chain to resume the drain —
// but operators forking/recovering via export must be aware of it.
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

// MaxNumKeysToMigratePerBlock bounds the governance-controlled rate. The value
// flows into MemiavlMigrationIterator.NextBatch, which preallocates a slice of
// that capacity; an unbounded uint64 (e.g. a fat-fingered or malicious
// proposal) would deterministically panic (makeslice: cap out of range) or OOM
// every validator at the same height — an unrecoverable chain halt. 1,000,000
// keys/block is far above any realistic drain rate yet caps the preallocation
// at a safe size, so we reject anything larger at proposal-validation time.
const MaxNumKeysToMigratePerBlock uint64 = 1_000_000

// ParamKeyTable returns the key table for the migration subspace.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable(
		paramtypes.NewParamSetPair(KeyNumKeysToMigratePerBlock, new(uint64), validateNumKeysToMigratePerBlock),
	)
}

// validateNumKeysToMigratePerBlock type-checks the value and bounds it to
// MaxNumKeysToMigratePerBlock. 0 means "paused"; any rate in [0, max] is a
// valid (consensus-deterministic) value. Rejecting oversized values here, at
// proposal-submission time, keeps them out of chain state where they would
// otherwise OOM/panic every validator (see MaxNumKeysToMigratePerBlock).
func validateNumKeysToMigratePerBlock(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v > MaxNumKeysToMigratePerBlock {
		return fmt.Errorf(
			"NumKeysToMigratePerBlock must be <= %d, got %d",
			MaxNumKeysToMigratePerBlock, v,
		)
	}
	return nil
}
