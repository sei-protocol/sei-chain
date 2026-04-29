package composite

import (
	"context"
	"fmt"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	dbm "github.com/tendermint/tm-db"
)

// Builds a router for the given write mode. A router is responsible for splitting reads/writes
// between the memiavl and flatkv backends.
func buildRouter(
	ctx context.Context,
	writeMode config.WriteMode,
	memIAVL *memiavl.CommitStore,
	flatkv *flatkv.CommitStore,
) (migration.Router, error) {

	switch writeMode {
	case config.MemIAVLOnly, config.FlatKVOnly, config.DualWrite:
		// A router is not needed when writing to only one DB or another.
		// The test mode "DualWrite" writes to both DBs, but doesn't use a router for the split.
		return nil, nil
	case config.MigrateEVM:
		router, err := buildMigrateEVMRouter(ctx, memIAVL, flatkv)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateEVMRouter: %w", err)
		}
		return router, nil
	case config.EVMMigrated:
		router, err := buildEvmMigratedRouter(ctx, memIAVL, flatkv)
		if err != nil {
			return nil, fmt.Errorf("buildEvmMigratedRouter: %w", err)
		}
		return router, nil
	case config.MigrateAllButBank:
		router, err := buildMigrateAllButBankRouter(ctx, memIAVL, flatkv)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateAllButBankRouter: %w", err)
		}
		return router, nil
	case config.AllMigratedButBank:
		router, err := buildAllButBankRouter(ctx, memIAVL, flatkv)
		if err != nil {
			return nil, fmt.Errorf("buildAllButBankRouter: %w", err)
		}
		return router, nil
	case config.MigrateBank:
		router, err := buildMigrateBankRouter(ctx, memIAVL, flatkv)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateBankRouter: %w", err)
		}
		return router, nil
	default:
		return nil, fmt.Errorf("unsupported write mode: %s", writeMode)
	}
}

/* Data flow: MigrateEVM (0 -> 1)

                       ┌──────────────┐                                  ┌─────────┐
──all-modules────────▶ │ moduleRouter │ ──everything-except-evm/───────▶ │ memIAVL │
                       └──────────────┘                                  └─────────┘
                              │                                               ▲
                             evm/                                             │
                              │       ┌──────un-migrated-keys─────────────────┘
                              │       │
                              ▼       │
                       ┌──────────────────┐                              ┌────────┐
                       │ migrationManager │ ────────migrated-keys──────▶ │ flatKV │
                       └──────────────────┘                              └────────┘
*/

// Build a router for handling write mode MigrateEVM. Migrates from version 0 to version 1.
func buildMigrateEVMRouter(
	ctx context.Context,
	memIAVL *memiavl.CommitStore,
	flatkv *flatkv.CommitStore,
) (migration.Router, error) {

	// Manages migration and routing for keys in the evm/ module.
	migrationManager, err := migration.NewMigrationManager(
		100, // TODO should be configurable
		Version0_MemiavlOnly,
		Version1_MigrateEVM,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatkv),
		buildFlatKVWriter(flatkv),
		migration.NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{keys.EVMStoreKey}),
		migration.NewMigrationMetrics(ctx, Version1_MigrateEVM, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	nonEVMModules, err := AllModulesExcept(evmStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := migrationManager.BuildRoute(evmStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := migration.NewModuleRouter(nonEVMRoute, evmRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

/* Data flow: EVMMigrated (1)

                       ┌──────────────┐                                  ┌─────────┐
──all-modules────────▶ │ moduleRouter │ ──everything-except-evm/───────▶ │ memIAVL │
                       └──────────────┘                                  └─────────┘
                              │
                              │
                              │
                              │
                              │
                              │                                          ┌────────┐
                              └────────────evm/────────────────────────▶ │ flatKV │
                                                                         └────────┘
*/

// Build a router for handling write mode EVMMigrated. Operates on a schema at migration version 1.
func buildEvmMigratedRouter(
	ctx context.Context,
	memIAVL *memiavl.CommitStore,
	flatkv *flatkv.CommitStore,
) (migration.Router, error) {

	nonEVMModules, err := AllModulesExcept(evmStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := routeToFlatKV(flatkv, evmStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	moduleRouter, err := migration.NewModuleRouter(nonEVMRoute, evmRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

/* Data flow: MigrateAllButBank (1 -> 2)

                       ┌──────────────┐                                  ┌─────────┐
──all-modules────────▶ │ moduleRouter │ ──────────────bank/────────────▶ │ memIAVL │
                       └──────────────┘                                  └─────────┘
                        │     │                                               ▲
                        │   all but                                           │
                        │   bank/ and evm/    ┌──────un-migrated-keys─────────┘
                        │     │               │
                        │     ▼               │
                        │   ┌──────────────────┐                         ┌────────┐
                        │   │ migrationManager │ ───migrated-keys──────▶ │ flatKV │
                        │   └──────────────────┘                         └────────┘
                        │                                                    ▲
                        │                                                    │
                        └────────────────────────────evm/────────────────────┘
*/

// Build a router for handling write mode MigrateAllButBank. Migrates from version 1 to version 2.
func buildMigrateAllButBankRouter(
	ctx context.Context,
	memIAVL *memiavl.CommitStore,
	flatkv *flatkv.CommitStore,
) (migration.Router, error) {

	allModulesButEvmAndBank, err := AllModulesExcept(evmStoreKey, bankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}

	// Manages migration and routing for all keys except evm/ (already migrated) and bank/ (not migrating yet)
	migrationManager, err := migration.NewMigrationManager(
		100, // TODO should be configurable
		Version1_MigrateEVM,
		Version2_MigrateAllButBank,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatkv),
		buildFlatKVWriter(flatkv),
		migration.NewMemiavlMigrationIterator(memIAVL.GetDB(), allModulesButEvmAndBank),
		migration.NewMigrationMetrics(ctx, Version2_MigrateAllButBank, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	bankRoute, err := routeToMemIAVL(memIAVL, bankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := routeToFlatKV(flatkv, evmStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	allOtherModulesRoute, err := migrationManager.BuildRoute(allModulesButEvmAndBank...)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := migration.NewModuleRouter(bankRoute, evmRoute, allOtherModulesRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

/* Data flow: AllMigratedButBank (2)

                       ┌──────────────┐                                  ┌─────────┐
──all-modules────────▶ │ moduleRouter │ ───bank/───────────────────────▶ │ memIAVL │
                       └──────────────┘                                  └─────────┘
                              │
                              │
                              │
                              │
                              │
                              │                                          ┌────────┐
                              └────────────all─but─bank/───────────────▶ │ flatKV │
                                                                         └────────┘
*/

// Build a router for handling write mode AllMigratedButBank. Operates on a schema at migration version 2.
func buildAllButBankRouter(
	ctx context.Context,
	memIAVL *memiavl.CommitStore,
	flatkv *flatkv.CommitStore,
) (migration.Router, error) {
	allButBankModules, err := AllModulesExcept(bankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonBankRoute, err := routeToFlatKV(flatkv, allButBankModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	bankRoute, err := routeToMemIAVL(memIAVL, bankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	moduleRouter, err := migration.NewModuleRouter(nonBankRoute, bankRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

/* Data flow: MigrateBank (2 -> 3)

                       ┌──────────────┐                                  ┌─────────┐
──all-modules────────▶ │ moduleRouter │                                  │ memIAVL │
                       └──────────────┘                                  └─────────┘
                        │     │                                               ▲
                        │   bank/             ┌──────un-migrated-keys─────────┘
                        │     │               │
                        │     ▼               │
                        │   ┌──────────────────┐                         ┌────────┐
                        │   │ migrationManager │ ───migrated-keys──────▶ │ flatKV │
                        │   └──────────────────┘                         └────────┘
                        │                                                    ▲
                        │                                                    │
                        └───────────────────all─but─bank/────────────────────┘
*/

// Build a router for handling write mode MigrateBank. Migrates from version 2 to version 3.
func buildMigrateBankRouter(
	ctx context.Context,
	memIAVL *memiavl.CommitStore,
	flatkv *flatkv.CommitStore,
) (migration.Router, error) {

	allButBankModules, err := AllModulesExcept(bankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}

	// Manages migration and routing for all keys except evm/ (already migrated) and bank/ (not migrating yet)
	migrationManager, err := migration.NewMigrationManager(
		100, // TODO should be configurable
		Version2_MigrateAllButBank,
		Version3_FlatKVOnly,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatkv),
		buildFlatKVWriter(flatkv),
		migration.NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{bankStoreKey}),
		migration.NewMigrationMetrics(ctx, Version3_FlatKVOnly, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	bankRoute, err := migrationManager.BuildRoute(bankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	allOtherModulesRoute, err := routeToFlatKV(flatkv, allButBankModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	moduleRouter, err := migration.NewModuleRouter(bankRoute, allOtherModulesRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

// Build a function capable of reading data from memiavl.
func buildMemIAVLReader(memIAVL *memiavl.CommitStore) migration.DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, false, fmt.Errorf("store not found: %s", store)
		}
		value := childStore.Get(key)
		return value, value != nil, nil
	}
}

// Build a function capable of writing data to memiavl.
func buildMemIAVLWriter(memIAVL *memiavl.CommitStore) migration.DBWriter {
	return func(ctx context.Context, changesets []*proto.NamedChangeSet) error {
		err := memIAVL.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a function capable of getting an iterator over a range of keys in a memiavl store.
func buildMemIAVLIteratorBuilder(memIAVL *memiavl.CommitStore) migration.DBIteratorBuilder {
	return func(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.Iterator(start, end, ascending), nil
	}
}

// Build a function capable of building a proof of the value for a key in a memiavl store.
func buildMemIAVLProofBuilder(memIAVL *memiavl.CommitStore) migration.DBProofBuilder {
	return func(store string, key []byte) (*ics23.CommitmentProof, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.GetProof(key), nil
	}
}

// Build a function capable of reading data from flatkv.
func buildFlatKVReader(flatkv *flatkv.CommitStore) migration.DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		value, found := flatkv.Get(store, key)
		return value, found, nil
	}
}

// Build a function capable of writing data to flatkv.
func buildFlatKVWriter(flatkv *flatkv.CommitStore) migration.DBWriter {
	return func(ctx context.Context, changesets []*proto.NamedChangeSet) error {
		err := flatkv.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a route to a memiavl store for the given module names.
func routeToMemIAVL(memIAVL *memiavl.CommitStore, moduleNames ...string) (*migration.Route, error) {
	return migration.NewRoute(
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		moduleNames...,
	)
}

// Build a route to a flatkv store for the given module names.
func routeToFlatKV(flatkv *flatkv.CommitStore, moduleNames ...string) (*migration.Route, error) {
	return migration.NewRoute(
		buildFlatKVReader(flatkv),
		buildFlatKVWriter(flatkv),
		nil, // iteration not supported
		nil, // proof building not supported
		moduleNames...,
	)
}
