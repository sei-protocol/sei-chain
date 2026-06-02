package migration

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
	dbm "github.com/tendermint/tm-db"
)

// Builds a router for the given migration write mode. A router is responsible for splitting
// reads/writes between the memiavl and flatkv backends.
func BuildRouter(
	ctx context.Context,
	writeMode config.WriteMode,
	memIAVL *memiavl.CommitStore,
	flatKV flatkv.Store,
	// If this router will be doing data migration, this is the number of keys to migrate in each batch.
	migrationBatchSize int,
) (Router, error) {

	switch writeMode {
	case config.MemiavlOnly:
		router, err := buildMemiavlOnlyRouter(memIAVL)
		if err != nil {
			return nil, fmt.Errorf("buildMemiavlOnlyRouter: %w", err)
		}
		return router, nil
	case config.MigrateEVM:
		router, err := buildMigrateEVMRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateEVMRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case config.EVMMigrated:
		router, err := buildEVMMigratedRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildEVMMigratedRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case config.MigrateAllButBank:
		router, err := buildMigrateAllButBankRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateAllButBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case config.AllMigratedButBank:
		router, err := buildAllMigratedButBankRouter(memIAVL, flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildAllMigratedButBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case config.MigrateBank:
		router, err := buildMigrateBankRouter(ctx, memIAVL, flatKV, migrationBatchSize)
		if err != nil {
			return nil, fmt.Errorf("buildMigrateBankRouter: %w", err)
		}
		threadSafe, err := NewThreadSafeRouter(router)
		if err != nil {
			return nil, fmt.Errorf("NewThreadSafeRouter: %w", err)
		}
		return threadSafe, nil
	case config.FlatKVOnly:
		router, err := buildFlatKVOnlyRouter(flatKV)
		if err != nil {
			return nil, fmt.Errorf("buildFlatKVOnlyRouter: %w", err)
		}
		return router, nil
	case config.TestOnlyDualWrite:
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
		buildMemIAVLIteratorBuilder(memIAVL),
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
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migrationBatchSize must be greater than 0")
	}

	memIAVLAcc := newAccumulatingWriter(buildMemIAVLWriter(memIAVL))
	flatKVAcc := newAccumulatingWriter(buildFlatKVWriter(flatKV))

	// Manages migration and routing for keys in the evm/ module.
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version0_MemiavlOnly,
		Version1_MigrateEVM,
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildFlatKVReader(flatKV),
		flatKVAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
		NewMemiavlMigrationIterator(memIAVL.GetDB(), []string{keys.EVMStoreKey}),
		NewMigrationMetrics(ctx, Version1_MigrateEVM, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	nonEVMModules, err := keys.AllModulesExcept(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := NewRoute(
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		nonEVMModules...,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	evmRoute, err := migrationManager.BuildRoute(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(nonEVMRoute, evmRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return newFlushingRouter(moduleRouter, memIAVLAcc.Flush, flatKVAcc.Flush), nil
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
	nonEVMRoute, err := NewRoute(
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		nonEVMModules...,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	evmRoute, err := NewRoute(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // iteration not supported by flatkv
		nil, // proof building not supported by flatkv
		keys.EVMStoreKey,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
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
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migrationBatchSize must be greater than 0")
	}

	allModulesButEvmAndBank, err := keys.AllModulesExcept(keys.EVMStoreKey, keys.BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}

	memIAVLAcc := newAccumulatingWriter(buildMemIAVLWriter(memIAVL))
	flatKVAcc := newAccumulatingWriter(buildFlatKVWriter(flatKV))

	// Manages migration and routing for all keys except evm/ (already migrated) and bank/ (not migrating yet)
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version1_MigrateEVM,
		Version2_MigrateAllButBank,
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildFlatKVReader(flatKV),
		flatKVAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
		NewMemiavlMigrationIterator(memIAVL.GetDB(), allModulesButEvmAndBank),
		NewMigrationMetrics(ctx, Version2_MigrateAllButBank, 10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("NewMigrationManager: %w", err)
	}

	bankRoute, err := NewRoute(
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		keys.BankStoreKey,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	evmRoute, err := NewRoute(
		buildFlatKVReader(flatKV),
		flatKVAcc.Apply,
		nil, // iteration not supported by flatkv
		nil, // proof building not supported by flatkv
		keys.EVMStoreKey,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	allOtherModulesRoute, err := migrationManager.BuildRoute(allModulesButEvmAndBank...)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(bankRoute, evmRoute, allOtherModulesRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return newFlushingRouter(moduleRouter, memIAVLAcc.Flush, flatKVAcc.Flush), nil
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

	// Steady-state mode: each backend is written by exactly one route
	// (flatkv for non-bank, memiavl for bank), so there is no fan-out to
	// coalesce and no accumulating writer is needed.
	nonBankRoute, err := NewRoute(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // iteration not supported by flatkv
		nil, // proof building not supported by flatkv
		allButBankModules...,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	bankRoute, err := NewRoute(
		buildMemIAVLReader(memIAVL),
		buildMemIAVLWriter(memIAVL),
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		keys.BankStoreKey,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
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
	if migrationBatchSize <= 0 {
		return nil, fmt.Errorf("migrationBatchSize must be greater than 0")
	}

	allButBankModules, err := keys.AllModulesExcept(keys.BankStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}

	memIAVLAcc := newAccumulatingWriter(buildMemIAVLWriter(memIAVL))
	flatKVAcc := newAccumulatingWriter(buildFlatKVWriter(flatKV))

	// Manages migration and routing for keys in the bank/ module (the
	// final module remaining in memiavl; every other module already
	// lives in flatkv from prior migrations).
	migrationManager, err := NewMigrationManager(
		migrationBatchSize,
		Version2_MigrateAllButBank,
		Version3_FlatKVOnly,
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildFlatKVReader(flatKV),
		flatKVAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
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

	allOtherModulesRoute, err := NewRoute(
		buildFlatKVReader(flatKV),
		flatKVAcc.Apply,
		nil, // iteration not supported by flatkv
		nil, // proof building not supported by flatkv
		allButBankModules...,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(bankRoute, allOtherModulesRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return newFlushingRouter(moduleRouter, memIAVLAcc.Flush, flatKVAcc.Flush), nil
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

	// Steady-state mode: a single route writes flatkv, so there is no
	// fan-out to coalesce and no accumulating writer is needed.
	router, err := NewPassthroughRouter(
		buildFlatKVReader(flatKV),
		buildFlatKVWriter(flatKV),
		nil, // iteration not supported by flatkv
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

	memIAVLAcc := newAccumulatingWriter(buildMemIAVLWriter(memIAVL))
	flatKVAcc := newAccumulatingWriter(buildFlatKVWriter(flatKV))

	// Sends evm/ traffic to both memIAVL and flatKV.
	// Note that a TestOnlyDualWriteRouter ignores module names; it's only job is to duplicate traffic.
	// The routes given to the dual write router do not specify modules for this reason.
	memiavlEvmRoute, err := NewRoute(
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}
	dualWriteRouter, err := NewTestOnlyDualWriteRouter(
		memiavlEvmRoute,
		flatKVAcc.Apply,
	)
	if err != nil {
		return nil, fmt.Errorf("NewTestOnlyDualWriteRouter: %w", err)
	}

	nonEVMModules, err := keys.AllModulesExcept(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("AllModulesExcept: %w", err)
	}
	nonEVMRoute, err := NewRoute(
		buildMemIAVLReader(memIAVL),
		memIAVLAcc.Apply,
		buildMemIAVLIteratorBuilder(memIAVL),
		buildMemIAVLProofBuilder(memIAVL),
		nonEVMModules...,
	)
	if err != nil {
		return nil, fmt.Errorf("NewRoute: %w", err)
	}

	evmRoute, err := dualWriteRouter.BuildRoute(keys.EVMStoreKey)
	if err != nil {
		return nil, fmt.Errorf("BuildRoute: %w", err)
	}

	moduleRouter, err := NewModuleRouter(nonEVMRoute, evmRoute)
	if err != nil {
		return nil, fmt.Errorf("NewModuleRouter: %w", err)
	}

	return newFlushingRouter(moduleRouter, memIAVLAcc.Flush, flatKVAcc.Flush), nil
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
	return func(changesets []*proto.NamedChangeSet, _ bool) error {
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
func buildFlatKVReader(flatKV flatkv.Store) DBReader {
	return func(store string, key []byte) ([]byte, bool, error) {
		value, found := flatKV.Get(store, key)
		return value, found, nil
	}
}

// Build a function capable of writing data to flatkv.
func buildFlatKVWriter(flatKV flatkv.Store) DBWriter {
	return func(changesets []*proto.NamedChangeSet, _ bool) error {
		err := flatKV.ApplyChangeSets(changesets)
		if err != nil {
			return fmt.Errorf("ApplyChangeSets: %w", err)
		}
		return nil
	}
}
