package migration

import (
	"context"
	"fmt"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	dbm "github.com/tendermint/tm-db"
)

// Builds a router for the given migration write mode. A router is responsible for splitting
// reads/writes between the memiavl and flatkv backends. Always returns a non-nil Router on
// success. Callers that intend to write to only memiavl or only flatkv must skip this entry
// point and use the underlying store handles directly.
func BuildRouter(
	ctx context.Context,
	writeMode WriteMode,
	memIAVL *memiavl.CommitStore,
	flatKV *flatkv.CommitStore,
	// If this router will be doing data migration, this is the number of keys to migrate in each batch.
	migrationBatchSize int,
) (Router, error) {

	switch writeMode {
	case MigrateEVM:
		router, err := buildMigrateEVMRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateEVMRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case EVMMigrated:
		router, err := buildEVMMigratedRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildEVMMigratedRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case MigrateAllButBank:
		router, err := buildMigrateAllButBankRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateAllButBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case AllMigratedButBank:
		router, err := buildAllMigratedButBankRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildAllMigratedButBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case MigrateBank:
		router, err := buildMigrateBankRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
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
	flatKV *flatkv.CommitStore,
	migrationBatchSize int,
) (Router, error) {

	// Manages migration and routing for keys in the evm/ module.
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version0_MemiavlOnly,
		Version1_MigrateEVM,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{EVMStoreKey}),
		NewMigrationMetrics(ctx, Version1_MigrateEVM, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	nonEVMModules, err := AllModulesExcept(EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := migrationManager.BuildRoute(EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(nonEVMRoute, evmRoute)
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
func buildEVMMigratedRouter(
	memIAVL *memiavl.CommitStore,
	flatKV *flatkv.CommitStore,
) (Router, error) {

	nonEVMModules, err := AllModulesExcept(EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := routeToFlatKV(flatKV, EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	moduleRouter, err := NewModuleRouter(nonEVMRoute, evmRoute)
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
	flatKV *flatkv.CommitStore,
	migrationBatchSize int,
) (Router, error) {

	allModulesButEvmAndBank, err := AllModulesExcept(EVMStoreKey, BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}

	// Manages migration and routing for all keys except evm/ (already migrated) and bank/ (not migrating yet)
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version1_MigrateEVM,
		Version2_MigrateAllButBank,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		NewMemiavlMigrationIterator(memIAVL.GetDB(), allModulesButEvmAndBank),
		NewMigrationMetrics(ctx, Version2_MigrateAllButBank, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	bankRoute, err := routeToMemIAVL(memIAVL, BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := routeToFlatKV(flatKV, EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	allOtherModulesRoute, err := migrationManager.BuildRoute(allModulesButEvmAndBank...)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(bankRoute, evmRoute, allOtherModulesRoute)
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
func buildAllMigratedButBankRouter(
	memIAVL *memiavl.CommitStore,
	flatKV *flatkv.CommitStore,
) (Router, error) {
	allButBankModules, err := AllModulesExcept(BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonBankRoute, err := routeToFlatKV(flatKV, allButBankModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	bankRoute, err := routeToMemIAVL(memIAVL, BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	moduleRouter, err := NewModuleRouter(nonBankRoute, bankRoute)
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
	flatKV *flatkv.CommitStore,
	migrationBatchSize int,
) (Router, error) {

	allButBankModules, err := AllModulesExcept(BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}

	// Manages migration and routing for keys in the bank/ module (the
	// final module remaining in memiavl; every other module already
	// lives in flatkv from prior migrations).
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version2_MigrateAllButBank,
		Version3_FlatKVOnly,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{BankStoreKey}),
		NewMigrationMetrics(ctx, Version3_FlatKVOnly, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	bankRoute, err := migrationManager.BuildRoute(BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	allOtherModulesRoute, err := routeToFlatKV(flatKV, allButBankModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	moduleRouter, err := NewModuleRouter(bankRoute, allOtherModulesRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

// Build a function capable of reading data from memiavl.
func buildMemIAVLReader(memIAVL *memiavl.CommitStore) DBReader {
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
func buildMemIAVLWriter(memIAVL *memiavl.CommitStore) DBWriter {
	return func(changesets []*proto.NamedChangeSet) error {
		err := memIAVL.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a function capable of getting an iterator over a range of keys in a memiavl store.
func buildMemIAVLIteratorBuilder(memIAVL *memiavl.CommitStore) DBIteratorBuilder {
	return func(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.Iterator(start, end, ascending), nil
	}
}

// Build a function capable of building a proof of the value for a key in a memiavl store.
func buildMemIAVLProofBuilder(memIAVL *memiavl.CommitStore) DBProofBuilder {
	return func(store string, key []byte) (*ics23.CommitmentProof, error) {
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.GetProof(key), nil
	}
}

// Build a function capable of reading data from flatkv.
func buildFlatKVReader(flatKV *flatkv.CommitStore) DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		value, found := flatKV.Get(store, key)
		return value, found, nil
	}
}

// Build a function capable of writing data to flatkv.
func buildFlatKVWriter(flatKV *flatkv.CommitStore) DBWriter {
	return func(changesets []*proto.NamedChangeSet) error {
		err := flatKV.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a route to a memiavl store for the given module names.
func routeToMemIAVL(memIAVL *memiavl.CommitStore, moduleNames ...string) (*Route, error) {
	return NewRoute(
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		moduleNames...,
	)
}

// Build a route to a flatkv store for the given module names.
func routeToFlatKV(flatKV *flatkv.CommitStore, moduleNames ...string) (*Route, error) {
	return NewRoute(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // iteration not supported
		nil, // proof building not supported
		moduleNames...,
	)
}
