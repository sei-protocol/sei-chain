package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	dbm "github.com/tendermint/tm-db"
)

// RawGlobalIterator returns an iterator over all committed keys across the
// data DBs (account, code, storage, misc), merged in global lexicographic
// order. Within each DB, keys are in Pebble order. Per-DB _meta/* keys are
// skipped. Pending writes are not visible. metadataDB is not included.
func (s *CommitStore) RawGlobalIterator() (dbm.Iterator, error) {
	// Read lock for the construction span: the returned iterator pins a Pebble
	// view and may then outlive a concurrent ApplyChangeSets/Commit.
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	// NewMergingIterator takes ownership of children and closes all of them if
	// construction fails, so we must not close them again here (Pebble's Close is
	// not idempotent and a double close could corrupt its iterator pool).
	merged, err := iterators.NewMergingIterator(true, children...)
	if err != nil {
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

	// Read lock for the construction span: buildEvmIterator/buildMiscDBLane
	// snapshot the pending-writes maps and pin the Pebble view here, so the
	// returned iterator may safely outlive a concurrent ApplyChangeSets/Commit.
	s.mu.RLock()
	defer s.mu.RUnlock()

	var iter dbm.Iterator
	var err error
	if store == keys.EVMStoreKey {
		iter, err = s.buildEvmIterator(start, end, ascending)
	} else {
		lowerBound, upperBound := moduleIteratorBounds(store, start, end)
		iter, err = s.buildMiscDBLane(store, lowerBound, upperBound, ascending)
	}
	if err != nil {
		return nil, err
	}
	// The underlying lane/merge/transform iterators report physical Pebble
	// bounds from Domain(); present the caller's logical [start, end) instead.
	return iterators.NewDomainIterator(iter, start, end)
}

/* Data flow: buildEvmIterator

buildCodeLane ──────────────┐
buildStorageLane ───────────┤
buildMiscDBLane (evm/) ───┼──► merge iterator ──► memiavl keys + values
buildAccountNonceLane ──────┤
buildAccountCodehashLane ───┘

* balance not iterated — not stored in FlatKV yet
*/

func (s *CommitStore) buildEvmIterator(
	start []byte,
	end []byte,
	ascending bool,
) (dbm.Iterator, error) {
	lanes := make([]dbm.Iterator, 0, 5)

	// Each optimized lane scans its own physical keyspace and re-labels rows to
	// a logical key. The codehash lane is the only one whose logical type byte
	// (0x08) differs from the physical byte it scans (account rows live under
	// 0x0a), so its bounds must be translated against the account keyspace.
	for _, laneSpec := range s.evmLaneSpecs() {
		lower, upper, empty, err := laneSpec.bounds(start, end)
		if err != nil {
			closeIterators(lanes)
			return nil, err
		}
		if empty {
			continue
		}
		lane, err := laneSpec.build(lower, upper, ascending)
		if err != nil {
			closeIterators(lanes)
			return nil, err
		}
		lanes = append(lanes, lane)
	}

	// Misc is the identity-mapped catch-all (no single type prefix), so it uses
	// the whole-range translation and is always built.
	miscLower, miscUpper := moduleIteratorBounds(keys.EVMStoreKey, start, end)
	miscLane, err := s.buildMiscDBLane(keys.EVMStoreKey, miscLower, miscUpper, ascending)
	if err != nil {
		closeIterators(lanes)
		return nil, err
	}
	lanes = append(lanes, miscLane)

	// TODO: once we move account balances to FlatKV, we need to add a lane for them here.

	// NewMergingIterator takes ownership of the lanes and closes all of them if
	// construction fails, so we must not close them again here (Pebble's Close is
	// not idempotent and a double close could corrupt its iterator pool).
	merged, err := iterators.NewMergingIterator(ascending, lanes...)
	if err != nil {
		return nil, fmt.Errorf("failed to create EVM merge iterator: %w", err)
	}
	return merged, nil
}

// evmLaneSpec describes one EVM iterator lane.
type evmLaneSpec struct {
	// logical is the type byte callers query with.
	logical keys.EVMKeyKind
	// physical is the type byte the lane's rows are stored under; equal to
	// logical for every lane except codehash, whose rows live in the account DB
	// under 0x0a.
	physical keys.EVMKeyKind
	// build constructs the iterator that scans the lane's physical keyspace.
	build func(lower []byte, upper []byte, ascending bool) (dbm.Iterator, error)
}

// bounds resolves the lane's logical and physical type bytes and translates the
// caller's logical [start,end) into this lane's physical [lower,upper) via
// evmLaneBounds. empty is true when the lane's span is disjoint from [start,end)
// and the lane should be skipped.
func (sp evmLaneSpec) bounds(start []byte, end []byte) (lower []byte, upper []byte, empty bool, err error) {
	// the logical prefix, i.e. the prefix from the perspective of the external caller
	logicalByte, ok := keys.EVMKeyPrefixByte(sp.logical)
	if !ok {
		return nil, nil, false, fmt.Errorf("no prefix byte for EVM key kind %v", sp.logical)
	}
	// the physical type byte the rows are stored under in the low level DB;
	// the full physical prefix is the module name "evm/" followed by this byte
	physByte, ok := keys.EVMKeyPrefixByte(sp.physical)
	if !ok {
		return nil, nil, false, fmt.Errorf("no prefix byte for EVM key kind %v", sp.physical)
	}
	lower, upper, empty = evmLaneBounds(start, end, logicalByte, physByte)
	return lower, upper, empty, nil
}

// evmLaneSpecs returns the optimized lanes, in no particular order (the merging
// iterator orders the combined output). The misc catch-all lane is handled
// separately because it has no single type prefix.
func (s *CommitStore) evmLaneSpecs() []evmLaneSpec {
	return []evmLaneSpec{
		{keys.EVMKeyStorage, keys.EVMKeyStorage, s.buildStorageLane},
		{keys.EVMKeyCode, keys.EVMKeyCode, s.buildCodeLane},
		{keys.EVMKeyCodeHash, ktype.EVMKeyAccount, s.buildAccountCodehashLane},
		{keys.EVMKeyNonce, ktype.EVMKeyAccount, s.buildAccountNonceLane},
	}
}

// evmLaneBounds maps the caller's logical [start,end) range to the physical
// [lower,upper) range for a single EVM lane. Physical keys are
// "evm/" + physByte + suffix while logical keys are logicalPrefix + suffix, so
// the suffix and intra-lane ordering are preserved: translating the clamped
// logical bounds yields physical bounds that select exactly the in-range rows.
func evmLaneBounds(
	// start is the inclusive lower bound of the caller's logical range; nil means unbounded.
	start []byte,
	// end is the exclusive upper bound of the caller's logical range; nil means unbounded.
	end []byte,
	// logicalPrefix is the lane's logical type byte (the prefix callers use, e.g. 0x08 for codehash).
	logicalPrefix byte,
	// physByte is the physical type byte the rows are stored under. It equals logicalPrefix for every
	// lane except codehash, whose rows live in the account DB under 0x0a.
	physByte byte,
) (
	// lower is the physical inclusive lower bound for the lane.
	lower []byte,
	// upper is the physical exclusive upper bound for the lane.
	upper []byte,
	// empty is true when [start,end) is disjoint from the lane's span, so the lane should be skipped.
	empty bool,
) {
	lp := []byte{logicalPrefix}
	lpEnd := ktype.PrefixEnd(lp)

	lo := lp
	if start != nil && bytes.Compare(start, lp) > 0 {
		lo = start
	}
	hi := lpEnd
	if end != nil && bytes.Compare(end, lpEnd) < 0 {
		hi = end
	}
	if bytes.Compare(lo, hi) >= 0 {
		return nil, nil, true
	}

	physPrefix := ktype.ModulePhysicalKey(keys.EVMStoreKey, []byte{physByte})
	lower = ktype.ModulePhysicalKey(keys.EVMStoreKey, append([]byte{physByte}, lo[1:]...))
	if bytes.Equal(hi, lpEnd) {
		upper = ktype.PrefixEnd(physPrefix)
	} else {
		upper = ktype.ModulePhysicalKey(keys.EVMStoreKey, append([]byte{physByte}, hi[1:]...))
	}
	return lower, upper, false
}

// moduleIteratorBounds translates caller logical [start, end) keys into physical
// bounds for iterating a module-prefixed keyspace in the data DBs.
func moduleIteratorBounds(store string, start, end []byte) (lowerBound, upperBound []byte) {
	modulePrefix := ktype.ModulePhysicalKey(store, nil)
	lowerBound = modulePrefix
	if start != nil {
		lowerBound = ktype.ModulePhysicalKey(store, start)
	}
	if end != nil {
		upperBound = ktype.ModulePhysicalKey(store, end)
	} else {
		upperBound = ktype.PrefixEnd(modulePrefix)
	}
	return lowerBound, upperBound
}

// serializeForIter is the shared pending-writes serializer for every lane. A
// delete (including a nil value, since IsDelete reports true for a nil VType)
// serializes to nil; the per-lane transform's len(value)==0 guard then drops
// it. Committed Pebble rows are never deletes because Commit physically removes
// deleted keys (see prepareBatch), so a non-empty value is always a live entry
// and never needs an IsDelete re-check after deserialization.
func serializeForIter[T vtype.VType](v T) ([]byte, error) {
	if v.IsDelete() {
		return nil, nil
	}
	return v.Serialize(), nil
}

// buildLane wires the common FlatKV iterator pipeline shared by every lane:
// a map iterator over the pending writes is merged (pending wins) with a Pebble
// iterator over the committed rows, then a transform iterator re-labels rows to
// their logical key, decodes the value, and drops tombstones. The per-lane
// serializer and transform supply the only behavior that differs between lanes.
func buildLane[T vtype.VType](
	pending map[string]T,
	db seidbtypes.KeyValueDB,
	lowerBound, upperBound []byte,
	ascending bool,
	serialize func(T) ([]byte, error),
	transform iterators.IteratorTransform,
) (dbm.Iterator, error) {
	pendingDataIterator, err := iterators.NewMapIterator(
		lowerBound, upperBound, ascending, serialize, pending)
	if err != nil {
		return nil, fmt.Errorf("failed to create pending iterator: %w", err)
	}

	pebbleIterator, err := db.NewIter(&seidbtypes.IterOptions{
		LowerBound: lowerBound,
		UpperBound: upperBound,
		Reverse:    !ascending,
	})
	if err != nil {
		_ = pendingDataIterator.Close()
		return nil, fmt.Errorf("failed to create pebble iterator: %w", err)
	}

	// NewMergingIterator takes ownership of its children and closes all of them
	// if construction fails, so we must not close pebbleIterator/pendingDataIterator
	// here too: pebbleIterator.Close is not idempotent (Pebble recycles iterators
	// into a pool), and a double close could corrupt that pool.
	mergingIterator, err := iterators.NewMergingIterator(ascending, pebbleIterator, pendingDataIterator)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge iterator: %w", err)
	}

	transformedIterator, err := iterators.NewTransformingIterator(mergingIterator, transform)
	if err != nil {
		_ = mergingIterator.Close()
		return nil, fmt.Errorf("failed to create transform iterator: %w", err)
	}
	return transformedIterator, nil
}

/* Data flow: buildMiscDBLane

  ┌────────────────────────┐       ┌───────────────────┐
  │ miscWrites (pending) │       │ miscDB (pebble) │
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
        physical key + serialized MiscData
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

func (s *CommitStore) buildMiscDBLane(
	store string,
	lowerBound, upperBound []byte,
	ascending bool,
) (dbm.Iterator, error) {
	transform := func(key []byte, value []byte) ([]byte, []byte, bool, error) {
		if len(value) == 0 {
			return nil, nil, true, nil
		}
		moduleName, logicalKey, err := ktype.StripModulePrefix(key)
		if err != nil {
			return nil, nil, false, err
		}
		if moduleName != store {
			return nil, nil, false, fmt.Errorf(
				"misc iterator key %q has module %q, expected %q",
				key, moduleName, store,
			)
		}
		ld, err := vtype.DeserializeMiscData(value)
		if err != nil {
			return nil, nil, false, err
		}
		return logicalKey, ld.GetValue(), false, nil
	}
	return buildLane(s.miscWrites, s.miscDB, lowerBound, upperBound, ascending, serializeForIter, transform)
}

/* Data flow: buildCodeLane

  ┌─────────────────────┐       ┌────────────────┐
  │ codeWrites (pending)│       │ codeDB (pebble)│
  └─────────────────────┘       └────────────────┘
             │                          │
             ▼                          ▼
      ┌──────────────┐          ┌─────────────────┐
      │ map iterator │          │ pebble iterator │
      └──────────────┘          └─────────────────┘
             │                          │
             └──────┐      ┌────────────┘
			        │      │
                    ▼      ▼
               ┌────────────────┐
               │ merge iterator │  pending writes "win"
               └────────────────┘
                        │
        physical key + serialized CodeData
		     includes deleted values
                        │
                        ▼
              ┌────────────────────┐
              │ transform iterator │
              └────────────────────┘
                        │
              0x07‖addr + bytecode
	         excludes deleted values
                        │
                        ▼
*/

func (s *CommitStore) buildCodeLane(
	lowerBound, upperBound []byte,
	ascending bool,
) (dbm.Iterator, error) {
	transform := func(key []byte, value []byte) ([]byte, []byte, bool, error) {
		if len(value) == 0 {
			return nil, nil, true, nil
		}
		_, strippedKey, err := ktype.StripEVMPhysicalKey(key)
		if err != nil {
			return nil, nil, false, err
		}
		cd, err := vtype.DeserializeCodeData(value)
		if err != nil {
			return nil, nil, false, err
		}
		return keys.BuildEVMKey(keys.EVMKeyCode, strippedKey), cd.GetBytecode(), false, nil
	}
	return buildLane(s.codeWrites, s.codeDB, lowerBound, upperBound, ascending, serializeForIter, transform)
}

/* Data flow: buildStorageLane

  ┌─────────────────────────┐       ┌────────────────────┐
  │ storageWrites (pending) │       │ storageDB (pebble) │
  └─────────────────────────┘       └────────────────────┘
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
        physical key + serialized StorageData
		     includes deleted values
                        │
                        ▼
              ┌────────────────────┐
              │ transform iterator │
              └────────────────────┘
                        │
           0x03‖addr‖slot + 32-byte value
	         excludes deleted values
                        │
                        ▼
*/

func (s *CommitStore) buildStorageLane(
	lowerBound, upperBound []byte,
	ascending bool,
) (dbm.Iterator, error) {
	transform := func(key []byte, value []byte) ([]byte, []byte, bool, error) {
		if len(value) == 0 {
			return nil, nil, true, nil
		}
		_, strippedKey, err := ktype.StripEVMPhysicalKey(key)
		if err != nil {
			return nil, nil, false, err
		}
		sd, err := vtype.DeserializeStorageData(value)
		if err != nil {
			return nil, nil, false, err
		}
		return keys.BuildEVMKey(keys.EVMKeyStorage, strippedKey), sd.GetValue()[:], false, nil
	}
	return buildLane(s.storageWrites, s.storageDB, lowerBound, upperBound, ascending, serializeForIter, transform)
}

/* Data flow: buildAccountNonceLane

  Same accountWrites + accountDB as buildAccountCodehashLane (one pending map, one DB).

  ┌─────────────────────────┐       ┌────────────────────┐
  │ accountWrites (pending) │       │ accountDB (pebble) │
  └─────────────────────────┘       └────────────────────┘
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
        physical key + serialized AccountData
		     includes deleted values
                        │
                        ▼
              ┌────────────────────┐
              │ transform iterator │
              └────────────────────┘
                        │
                 0x0a‖addr + 8-byte nonce
	         excludes deleted values
                        │
                        ▼
*/

func (s *CommitStore) buildAccountNonceLane(
	lowerBound, upperBound []byte,
	ascending bool,
) (dbm.Iterator, error) {
	transform := func(key []byte, value []byte) ([]byte, []byte, bool, error) {
		if len(value) == 0 {
			return nil, nil, true, nil
		}
		_, addrBytes, err := ktype.StripEVMPhysicalKey(key)
		if err != nil {
			return nil, nil, false, err
		}
		ad, err := vtype.DeserializeAccountData(value)
		if err != nil {
			return nil, nil, false, err
		}
		nonceBytes := make([]byte, vtype.NonceLen)
		binary.BigEndian.PutUint64(nonceBytes, ad.GetNonce())
		return keys.BuildEVMKey(keys.EVMKeyNonce, addrBytes), nonceBytes, false, nil
	}
	return buildLane(s.accountWrites, s.accountDB, lowerBound, upperBound, ascending, serializeForIter, transform)
}

/* Data flow: buildAccountCodehashLane

  Same accountWrites + accountDB as buildAccountNonceLane (one pending map, one DB).

  ┌─────────────────────────┐       ┌────────────────────┐
  │ accountWrites (pending) │       │ accountDB (pebble) │
  └─────────────────────────┘       └────────────────────┘
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
        physical key + serialized AccountData
		     includes deleted values
                        │
                        ▼
              ┌────────────────────┐
              │ transform iterator │
              └────────────────────┘
                        │
              0x08‖addr + code hash bytes
	         excludes deleted values and zero hash
                        │
                        ▼
*/

func (s *CommitStore) buildAccountCodehashLane(
	lowerBound, upperBound []byte,
	ascending bool,
) (dbm.Iterator, error) {
	transform := func(key []byte, value []byte) ([]byte, []byte, bool, error) {
		if len(value) == 0 {
			return nil, nil, true, nil
		}
		_, addrBytes, err := ktype.StripEVMPhysicalKey(key)
		if err != nil {
			return nil, nil, false, err
		}
		ad, err := vtype.DeserializeAccountData(value)
		if err != nil {
			return nil, nil, false, err
		}
		codeHash := ad.GetCodeHash()
		var zeroCodeHash vtype.CodeHash
		if *codeHash == zeroCodeHash {
			return nil, nil, true, nil
		}
		return keys.BuildEVMKey(keys.EVMKeyCodeHash, addrBytes), codeHash[:], false, nil
	}
	return buildLane(s.accountWrites, s.accountDB, lowerBound, upperBound, ascending, serializeForIter, transform)
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
