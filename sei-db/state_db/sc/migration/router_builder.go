package migration

import (
	"context"
	"fmt"
	"time"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// Builds a router for the given migration write mode. A router is responsible for splitting
// reads/writes between the memiavl and flatkv backends.
func BuildRouter(
	ctx context.Context,
	writeMode types.WriteMode,
	memIAVL *memiavl.CommitStore,
	flatKV flatkv.Store,
	// If this router will be doing data migration, this is the number of keys to migrate in each batch.
	migrationBatchSize int,
) (Router, error) {

	switch writeMode {
	case types.MemiavlOnly:
		router, err := buildMemiavlOnlyRouter(memIAVL)
		if err != nil {
			return nil, fmt.Errorf("buildMemiavlOnlyRouter: %w", err)
		}
		return router, nil
	case types.MigrateEVM:
		router, err := buildMigrateEVMRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateEVMRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case types.EVMMigrated:
		router, err := buildEVMMigratedRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildEVMMigratedRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case types.MigrateAllButBank:
		router, err := buildMigrateAllButBankRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateAllButBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case types.AllMigratedButBank:
		router, err := buildAllMigratedButBankRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildAllMigratedButBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case types.MigrateBank:
		router, err := buildMigrateBankRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case types.FlatKVOnly:
		router, err := buildFlatKVOnlyRouter(flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildFlatKVOnlyRouter: %w", err)
		}
		return router, nil
	case types.TestOnlyDualWrite:
		router, err := buildTestOnlyDualWriteRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildTestOnlyDualWriteRouter: %w", err)
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

/* Data flow: MemiavlOnly (0)

                       ┌─────────────┐                                  ┌─────────┐
──all-modules────────▶ │ passthrough │ ──────────all-modules──────────▶ │ memIAVL │
                       └─────────────┘                                  └─────────┘
*/

// Build a router for handling write mode MemiavlOnly. Operates on a schema at migration version 0.
func buildMemiavlOnlyRouter(
	memIAVL *memiavl.CommitStore,
) (Router, error) {
	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}

	router, err := NewPassthroughRouter(
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
	)
	if err != nil {
		return nil, fmt.Errorf("NewPassthroughRouter: %w", err)
	}

	return router, nil
}

/* Data flow: FlatKV EVM migrate (0 -> 1)

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
	flatKV flatkv.Store,
	migrationBatchSize int,
) (Router, error) {

	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}
	// Manages migration and routing for keys in the evm/ module.
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version0_MemiavlOnly,
		Version1_MigrateEVM,
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{keys.EVMStoreKey}),
		NewMigrationMetrics(ctx, Version1_MigrateEVM, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}
	readonly := memIAVL.GetDB().ReadOnly()
	if !readonly {
		logger.Info("created new EVM migration manager",
			"startVersion", Version0_MemiavlOnly,
			"targetVersion", Version1_MigrateEVM,
			"boundary", migrationManager.boundary.String())
	}

	nonEVMModules, err := keys.AllModulesExcept(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := migrationManager.BuildRoute(keys.EVMStoreKey)
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
	flatKV flatkv.Store,
) (Router, error) {

	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}

	nonEVMModules, err := keys.AllModulesExcept(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := routeToFlatKV(flatKV, keys.EVMStoreKey)
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
	flatKV flatkv.Store,
	migrationBatchSize int,
) (Router, error) {

	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}
	allModulesButEvmAndBank, err := keys.AllModulesExcept(keys.EVMStoreKey, keys.BankStoreKey)
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

	bankRoute, err := routeToMemIAVL(memIAVL, keys.BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := routeToFlatKV(flatKV, keys.EVMStoreKey)
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
	flatKV flatkv.Store,
) (Router, error) {

	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}

	allButBankModules, err := keys.AllModulesExcept(keys.BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonBankRoute, err := routeToFlatKV(flatKV, allButBankModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToFlatKV: %w", err)
	}

	bankRoute, err := routeToMemIAVL(memIAVL, keys.BankStoreKey)
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
	flatKV flatkv.Store,
	migrationBatchSize int,
) (Router, error) {

	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}
	allButBankModules, err := keys.AllModulesExcept(keys.BankStoreKey)
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
		NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{keys.BankStoreKey}),
		NewMigrationMetrics(ctx, Version3_FlatKVOnly, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	bankRoute, err := migrationManager.BuildRoute(keys.BankStoreKey)
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

/* Data flow: FlatKVOnly (3)

                       ┌─────────────┐                                  ┌────────┐
──all-modules────────▶ │ passthrough │ ──────────all-modules──────────▶ │ flatKV │
                       └─────────────┘                                  └────────┘
*/

// Build a router for handling write mode FlatKVOnly. Operates on a schema at migration version 3.
func buildFlatKVOnlyRouter(
	flatKV flatkv.Store,
) (Router, error) {
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}

	router, err := NewPassthroughRouter(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // proof building not supported by flatkv
	)
	if err != nil {
		return nil, fmt.Errorf("NewPassthroughRouter: %w", err)
	}

	return router, nil
}

/* Data flow: dual write (test only)

                       ┌──────────────┐                                  ┌─────────┐
──all-modules────────▶ │ moduleRouter │ ──everything-except-evm/───────▶ │ memIAVL │
                       └──────────────┘                                  └─────────┘
                              │                                               ▲
                             evm/                                             │
                              │       ┌──────evm/─reads-and-writes────────────┘
                              │       │
                              ▼       │
                       ┌───────────────────┐                             ┌────────┐
                       │ dual write router │ ───────evm/-writes────────▶ │ flatKV │
                       └───────────────────┘                             └────────┘
*/

// Build a test-only dual-write router.
//
// CRITICAL: this is a test-only router and should never be deployed to production machines.
func buildTestOnlyDualWriteRouter(
	memIAVL *memiavl.CommitStore,
	flatKV flatkv.Store,
) (Router, error) {
	if memIAVL == nil {
		return nil, fmt.Errorf("memIAVL is nil")
	}
	if flatKV == nil {
		return nil, fmt.Errorf("flatKV is nil")
	}

	// Sends evm/ traffic to both memIAVL and flatKV.
	// Note that a TestOnlyDualWriteRouter ignores module names; it's only job is to duplicate traffic.
	// The routes given to the dual write router do not specify modules for this reason.
	memiavlEvmRoute, err := routeToMemIAVL(memIAVL)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}
	dualWriteRouter, err := NewTestOnlyDualWriteRouter(
		memiavlEvmRoute,
		buildFlatKVWriter(flatKV),
	)
	if err != nil {
		return nil, fmt.Errorf("NewTestOnlyDualWriteRouter: %w", err)
	}

	nonEVMModules, err := keys.AllModulesExcept(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := routeToMemIAVL(memIAVL, nonEVMModules...)
	if err != nil {
		return nil, fmt.Errorf("routeToMemIAVL: %w", err)
	}

	evmRoute, err := dualWriteRouter.BuildRoute(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(nonEVMRoute, evmRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return moduleRouter, nil
}

// Build a function capable of reading data from memiavl.
//
// During state-sync the underlying memiavl DB may not yet be open: the
// snapshot is still being applied while the mempool reactor is already
// dispatching CheckTx calls. Treat that pre-load window as "no committed
// state" by reporting key-not-found rather than erroring; once LoadVersion
// opens the DB the original "store not found" config-error path resumes.
func buildMemIAVLReader(memIAVL *memiavl.CommitStore) DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		if !memIAVL.IsLoaded() {
			return nil, false, nil
		}
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, false, fmt.Errorf("store not found: %s", store)
		}
		value := childStore.Get(key)
		return value, value != nil, nil
	}
}

// Build a function capable of writing data to memiavl.
//
// Writes should never reach this closure before memiavl is loaded: the
// commit pipeline only runs after the snapshot has been applied. Return a
// loud error if it ever happens so the bug surfaces instead of corrupting
// silently.
func buildMemIAVLWriter(memIAVL *memiavl.CommitStore) DBWriter {
	return func(changesets []*proto.NamedChangeSet, _ bool) error {
		if !memIAVL.IsLoaded() {
			return fmt.Errorf("memiavl commit store not loaded yet; refusing to apply %d changeset(s)", len(changesets))
		}
		err := memIAVL.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}

// Build a function capable of building a proof of the value for a key in a memiavl store.
func buildMemIAVLProofBuilder(memIAVL *memiavl.CommitStore) DBProofBuilder {
	return func(store string, key []byte) (*ics23.CommitmentProof, error) {
		if !memIAVL.IsLoaded() {
			return nil, fmt.Errorf("memiavl commit store not loaded yet; cannot build proof for store %q", store)
		}
		childStore := memIAVL.GetChildStoreByName(store)
		if childStore == nil {
			return nil, fmt.Errorf("store not found: %s", store)
		}
		return childStore.GetProof(key), nil
	}
}

// Build a function capable of reading data from flatkv.
func buildFlatKVReader(flatKV flatkv.Store) DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		value, found := flatKV.Get(store, key)
		return value, found, nil
	}
}

// Build a function capable of writing data to flatkv.
func buildFlatKVWriter(flatKV flatkv.Store) DBWriter {
	return func(changesets []*proto.NamedChangeSet, _ bool) error {
		// Stamp at the next commit height so Apply/Commit versions match
		// under the sequential composite commit path.
		err := flatKV.ApplyChangeSets(flatKV.Version()+1, changesets)
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
		buildMemIAVLProofBuilder(memIAVL),
		moduleNames...,
	)
}

// Build a route to a flatkv store for the given module names.
func routeToFlatKV(flatKV flatkv.Store, moduleNames ...string) (*Route, error) {
	return NewRoute(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // proof building not supported
		moduleNames...,
	)
}
