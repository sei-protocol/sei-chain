package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	dbm "github.com/tendermint/tm-db"
)

// RawGlobalIterator returns an iterator over all committed keys across the
// data DBs (account, code, storage, legacy), merged in global lexicographic
// order. Within each DB, keys are in Pebble order. Per-DB _meta/* keys are
// skipped. Pending writes are not visible. metadataDB is not included.
func (s *CommitStore) RawGlobalIterator() (dbm.Iterator, error) {
	dbs := s.dataDBs()
	children := make([]dbm.Iterator, 0, len(dbs))
	for _, db := range dbs {
		pebbleIter, err := db.NewIter(nil)
		if err != nil {
			closeIterators(children)
			return nil, fmt.Errorf("open data DB iterator: %w", err)
		}
		transformed, err := iterators.NewTransformingIterator(pebbleIter, skipMetaKeys)
		if err != nil {
			closeIterators(children)
			return nil, err
		}
		children = append(children, transformed)
	}
	merged, err := iterators.NewMergingIterator(true, children...)
	if err != nil {
		closeIterators(children)
		return nil, err
	}
	if err := merged.Error(); err != nil {
		_ = merged.Close()
		return nil, err
	}
	return merged, nil
}

func (s *CommitStore) Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error) {
	if store == "" {
		return nil, fmt.Errorf("store name cannot be empty")
	}

	if store == keys.EVMStoreKey {
		return s.buildEvmIterator(start, end, ascending)
	} else {
		return s.buildLegacyIterator(store, start, end, ascending)
	}
}

/* Data flow: buildLegacyIterator (non-EVM modules)

  ┌──────────────────────┐       ┌─────────────────┐
  │ codeWrites (pending) │       │ codeDB (pebble) │
  └──────────────────────┘       └─────────────────┘
             │                           │
             ▼                           ▼
      ┌──────────────┐          ┌─────────────────┐
      │ map iterator │          │ pebble iterator │
      └──────────────┘          └─────────────────┘
             │                           │
             └──────┐      ┌─────────────┘
			        │      │
                    ▼      ▼
               ┌────────────────┐
               │ merge iterator │  pending writes "win"
               └────────────────┘
                        │
        physical key + serialized code data
		     includes deleted values
                        │
                        ▼
              ┌────────────────────┐
              │ transform iterator │
              └────────────────────┘
                        │
       logical module key + raw value bytes
	         excludes deleted values
                        │
                        └--------------------------------------------------------------------------------------------┐
                                                                                                                     │
                                                                                                                     │
  ┌─────────────────────────┐   ┌────────────────────┐                                                               │
  │ storageWrites (pending) │   │ storageDB (pebble) │                                                               │
  └─────────────────────────┘   └────────────────────┘                                                               │
             │                           │                                                                           │
             ▼                           ▼                                                                           │
      ┌──────────────┐          ┌─────────────────┐                                                                  │
      │ map iterator │          │ pebble iterator │                                                                  │
      └──────────────┘          └─────────────────┘                                                                  │
             │                           │                                                                           │
             └──────┐      ┌─────────────┘                                                                           │
			        │      │                                                                                         │
                    ▼      ▼                                                                                         │
               ┌────────────────┐                                                                                    │
               │ merge iterator │  pending writes "win"                                                              │
               └────────────────┘                                                                                    │
                        │                                                                                            │
        physical key + serialized storage data                                                                       │
		     includes deleted values                                                                                 │
                        │                                                                                            │
                        ▼                                                                                            │
              ┌────────────────────┐                                                                                 │
              │ transform iterator │                                                                                 │
              └────────────────────┘                                                                                 │
                        │                                                                                            │
       logical module key + raw value bytes                                                                          │                                                                                    │
	         excludes deleted values                                                                                 │
                        │                                                                                            │
                        └----------------------------------------------------------------------------------------┐   │
                                                                                                                 │   │
                                                                                                                 │   │
            ┌─────────────────────────┐                            ┌──────────────────────────┐                  │   │
            │ accountWrites (pending) │                            │    accountDB (pebble)    │                  │   │
            └─────────────────────────┘                            └──────────────────────────┘                  │   │
             │           │           │                              │           │           │                    │   │
             ▼           ▼           ▼                              ▼           ▼           ▼                    │   │
┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐   │   │
│ map iterator │ │ map iterator │ │ map iterator │ │ pebble iterator │ │ pebble iterator │ │ pebble iterator │   │   │
└──────────────┘ └──────────────┘ └──────────────┘ └─────────────────┘ └─────────────────┘ └─────────────────┘   │   │
    │                 │                  │                          │                 │                  │       │   │
	│    ┌──────────────────────────────────────────────────────────┘                 │                  │       │   │
	│    │            │                  │                                            │                  │       │   │
	│    │            │        ┌──────────────────────────────────────────────────────┘                  │       │   │
	│    │            │        │         │                                                               │       │   │
	│    │            │        │         │        ┌──────────────────────────────────────────────────────┘       │   │
	│    │            │        │         │        │                                                              │   │
    ▼    ▼            ▼        ▼         ▼        ▼                                                              │   │
┌────────────────┐ ┌────────────────┐ ┌────────────────┐                                                         │   │
│ merge iterator │ │ merge iterator │ │ merge iterator │ pending writes "win"                                    │   │
└────────────────┘ └────────────────┘ └────────────────┘                                                         │   │
             |               |                     |                                                             │   │
	         |               |                     |                                                             │   │
	physical key + full serialized account data, includes deletions                                              │   │
	         |               |                     |                                                             │   │
		     |               |                     |                                                             │   │
             ▼               ▼                     ▼                                                             │   │
┌────────────────────┐ ┌────────────────────┐ ┌────────────────────┐                                             │   │
│ transform iterator │ │ transform iterator │ │ transform iterator │                                             │   │
└────────────────────┘ └────────────────────┘ └────────────────────┘                                             │   │
             |               |                     |                                                             │   │
			 |               |                     |                                                             │   │
         balance*          nonce                codehash                                                         │   │
	   logical key      logical key           logical key                                                        │   │
	   no deletions     no deletions          no deletions                                                       │   │
             |               |                     |                                                             │   │
			 |               |                     |                                                             │   │
			 ▼               ▼                     ▼                                                             ▼   ▼
┌──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                                    merge iterator                                                    │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
                                                           │
                                                           ▼
*/

// Build an iterator that can walk evm data
func (s *CommitStore) buildEvmIterator(
	start []byte,
	end []byte,
	ascending bool,
) (dbm.Iterator, error) {

	return nil, nil
}

/* Data flow: buildLegacyIterator (non-EVM modules)

  ┌────────────────────────┐       ┌───────────────────┐
  │ legacyWrites (pending) │       │ legacyDB (pebble) │
  └────────────────────────┘       └───────────────────┘
             │                              │
             ▼                              ▼
      ┌──────────────┐             ┌─────────────────┐
      │ map iterator │             │ pebble iterator │
      └──────────────┘             └─────────────────┘
             │                              │
             └──────┐      ┌────────────────┘
			        │      │
                    ▼      ▼
               ┌────────────────┐
               │ merge iterator │  pending writes "win"
               └────────────────┘
                        │
        physical key + serialized LegacyData
		     includes deleted values
                        │
                        ▼
              ┌────────────────────┐
              │ transform iterator │
              └────────────────────┘
                        │
       logical module key + raw value bytes
	         excludes deleted values
                        │
                        ▼
*/

// Build an iterator that can walk non-EVM data (which is called legacy data in the codebase)
func (s *CommitStore) buildLegacyIterator(
	store string,
	start []byte,
	end []byte,
	ascending bool,
) (dbm.Iterator, error) {

	modulePrefix := ktype.ModulePhysicalKey(store, nil)
	lowerBound := modulePrefix
	if start != nil {
		lowerBound = ktype.ModulePhysicalKey(store, start)
	}
	var upperBound []byte
	if end != nil {
		upperBound = ktype.ModulePhysicalKey(store, end)
	} else {
		upperBound = ktype.PrefixEnd(modulePrefix)
	}

	// Create an iterator that walks the pending writes.
	serializer := func(v *vtype.LegacyData) ([]byte, error) {
		if v == nil {
			return nil, nil
		}
		return v.Serialize(), nil
	}
	pendingDataIterator, err := iterators.NewMapIterator(lowerBound, upperBound, ascending, serializer, s.legacyWrites)
	if err != nil {
		return nil, fmt.Errorf("failed to create pending data iterator: %w", err)
	}

	// Create an iterator that walks the data in pebble.
	pebbleIterator, err := s.legacyDB.NewIter(&seidbtypes.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
		Reverse:    !ascending,
	})
	if err != nil {
		_ = pendingDataIterator.Close()
		return nil, fmt.Errorf("failed to create pebble iterator: %w", err)
	}

	// Pebble first, pending second: the rightmost child (pending) wins on duplicate keys.
	mergingIterator, err := iterators.NewMergingIterator(ascending, pebbleIterator, pendingDataIterator)
	if err != nil {
		_ = pendingDataIterator.Close()
		_ = pebbleIterator.Close()
		return nil, fmt.Errorf("failed to create merging iterator: %w", err)
	}

	// Transform data into the form expected by the caller and skip deleted keys.
	transform := func(key []byte, value []byte) ([]byte, []byte, bool, error) {
		moduleName, logicalKey, err := ktype.StripModulePrefix(key)
		if err != nil {
			return nil, nil, false, err
		}
		if moduleName != store {
			return nil, nil, false, fmt.Errorf(
				"legacy iterator key %q has module %q, expected %q",
				key, moduleName, store,
			)
		}
		ld, err := vtype.DeserializeLegacyData(value)
		if err != nil {
			return nil, nil, false, err
		}
		if ld.IsDelete() {
			return nil, nil, true, nil
		}
		return logicalKey, ld.GetValue(), false, nil
	}
	transformedIterator, err := iterators.NewTransformingIterator(mergingIterator, transform)
	if err != nil {
		_ = mergingIterator.Close()
		return nil, fmt.Errorf("failed to create transformed iterator: %w", err)
	}

	return transformedIterator, nil
}

// Used to cause the raw global iterator to skip _meta/* keys.
func skipMetaKeys(key, value []byte) ([]byte, []byte, bool, error) {
	return key, value, ktype.IsMetaKey(key), nil
}

func closeIterators(iters []dbm.Iterator) {
	for _, it := range iters {
		if it != nil {
			_ = it.Close()
		}
	}
}
