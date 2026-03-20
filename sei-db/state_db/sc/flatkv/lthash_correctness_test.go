package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl/proto"
	"github.com/stretchr/testify/require"
)

// fullScanLtHash computes an LtHash from scratch by iterating every KV pair
// in accountDB, codeDB, and storageDB. This is the "ground truth" that an
// incremental LtHash must match after any sequence of apply+commit cycles.
func fullScanLtHash(t *testing.T, s *CommitStore) *lthash.LtHash {
	t.Helper()
	var pairs []lthash.KVPairWithLastValue

	scanDB := func(db types.KeyValueDB) {
		iter, err := db.NewIter(&types.IterOptions{})
		require.NoError(t, err)
		defer iter.Close()
		for iter.First(); iter.Valid(); iter.Next() {
			if isMetaKey(iter.Key()) {
				continue
			}
			key := bytes.Clone(iter.Key())
			value := bytes.Clone(iter.Value())
			pairs = append(pairs, lthash.KVPairWithLastValue{
				Key:   key,
				Value: value,
			})
		}
		require.NoError(t, iter.Error())
	}

	scanDB(s.accountDB)
	scanDB(s.codeDB)
	scanDB(s.storageDB)
	scanDB(s.legacyDB)

	result, _ := lthash.ComputeLtHash(nil, pairs)
	return result
}

// ---------- helpers to build memiavl-format changeset pairs ----------

func nonceBytes(n uint64) []byte {
	b := make([]byte, NonceLen)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func addrN(n byte) Address {
	var a Address
	a[19] = n
	return a
}

func slotN(n byte) Slot {
	var s Slot
	s[31] = n
	return s
}

func codeHashN(n byte) CodeHash {
	var h CodeHash
	for i := range h {
		h[i] = n
	}
	return h
}

func noncePair(addr Address, nonce uint64) *iavl.KVPair {
	return &iavl.KVPair{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
		Value: nonceBytes(nonce),
	}
}

func codeHashPair(addr Address, ch CodeHash) *iavl.KVPair {
	return &iavl.KVPair{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]),
		Value: ch[:],
	}
}

func codePair(addr Address, bytecode []byte) *iavl.KVPair {
	return &iavl.KVPair{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
		Value: bytecode,
	}
}

func codeDeletePair(addr Address) *iavl.KVPair {
	return &iavl.KVPair{
		Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:]),
		Delete: true,
	}
}

func storagePair(addr Address, slot Slot, val []byte) *iavl.KVPair {
	return &iavl.KVPair{
		Key:   evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)),
		Value: val,
	}
}

func storageDeletePair(addr Address, slot Slot) *iavl.KVPair {
	return &iavl.KVPair{
		Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot)),
		Delete: true,
	}
}

func nonceDeletePair(addr Address) *iavl.KVPair {
	return &iavl.KVPair{
		Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:]),
		Delete: true,
	}
}

func codeHashDeletePair(addr Address) *iavl.KVPair {
	return &iavl.KVPair{
		Key:    evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:]),
		Delete: true,
	}
}

func namedCS(pairs ...*iavl.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
}

// ---------- The main 100-block test ----------

// TestLtHashIncrementalEqualsFullScan runs 100 blocks that exercise every
// combination of account/code/storage add, update, delete — including the two
// scenarios that were previously buggy:
//
//  1. New account creation (phantom MixOut of zero AccountValue).
//  2. Multiple ApplyChangeSets calls per block for the same account (double MixOut).
//
// After all 100 blocks, the incremental workingLtHash must equal a full scan.
// We also verify at intermediate checkpoints (every 10 blocks).
func TestLtHashIncrementalEqualsFullScan(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	// ── Blocks 1-10: Create new accounts (EOA, only nonce) ──────────
	// This is the primary Bug 1 trigger: new accounts that don't exist in DB.
	for i := 1; i <= 10; i++ {
		addr := addrN(byte(i))
		cs := namedCS(noncePair(addr, uint64(i)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 10)

	// ── Blocks 11-20: Add storage slots for existing accounts ───────
	for i := 11; i <= 20; i++ {
		addr := addrN(byte(i - 10)) // accounts 1-10
		slot := slotN(byte(i))
		val := []byte{byte(i), 0xAA}
		cs := namedCS(storagePair(addr, slot, val))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 20)

	// ── Blocks 21-30: Deploy code to accounts (makes them contracts) ─
	// This modifies accountDB (codehash changes) AND codeDB.
	for i := 21; i <= 30; i++ {
		addr := addrN(byte(i - 20)) // accounts 1-10
		ch := codeHashN(byte(i))
		bytecode := []byte{0x60, 0x80, byte(i)}
		cs := namedCS(
			codeHashPair(addr, ch),
			codePair(addr, bytecode),
		)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 30)

	// ── Blocks 31-40: Update nonces on existing accounts ────────────
	for i := 31; i <= 40; i++ {
		addr := addrN(byte(i - 30)) // accounts 1-10
		cs := namedCS(noncePair(addr, uint64(i*100)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 40)

	// ── Blocks 41-50: Update storage values ─────────────────────────
	for i := 41; i <= 50; i++ {
		addr := addrN(byte(i - 40))
		slot := slotN(byte(i - 30)) // same slots created in blocks 11-20
		val := []byte{byte(i), 0xBB, 0xCC}
		cs := namedCS(storagePair(addr, slot, val))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 50)

	// ── Blocks 51-60: Delete some storage slots ─────────────────────
	for i := 51; i <= 55; i++ {
		addr := addrN(byte(i - 50))
		slot := slotN(byte(i - 40))
		cs := namedCS(storageDeletePair(addr, slot))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	// Blocks 56-60: add new storage to different slots
	for i := 56; i <= 60; i++ {
		addr := addrN(byte(i - 50))
		slot := slotN(byte(i + 100)) // new slot IDs
		val := []byte{byte(i), 0xDD}
		cs := namedCS(storagePair(addr, slot, val))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 60)

	// ── Blocks 61-70: Multiple ApplyChangeSets per block (Bug 2) ────
	// Same account is modified in two separate ApplyChangeSets calls
	// within a single block.
	for i := 61; i <= 70; i++ {
		addr := addrN(byte(i - 60))

		// First call: update nonce
		cs1 := namedCS(noncePair(addr, uint64(i*1000)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		// Second call: update codehash (same account, same block)
		ch := codeHashN(byte(i + 100))
		cs2 := namedCS(codeHashPair(addr, ch))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 70)

	// ── Blocks 71-75: Delete code from some accounts ────────────────
	for i := 71; i <= 75; i++ {
		addr := addrN(byte(i - 70))
		cs := namedCS(
			codeDeletePair(addr),
			codeHashDeletePair(addr),
		)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	// ── Blocks 76-80: Create brand new accounts (11-15) ─────────────
	for i := 76; i <= 80; i++ {
		addr := addrN(byte(i - 65)) // addresses 11-15
		cs := namedCS(noncePair(addr, uint64(i)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 80)

	// ── Blocks 81-85: Re-create previously deleted storage ──────────
	for i := 81; i <= 85; i++ {
		addr := addrN(byte(i - 80))
		slot := slotN(byte(i - 70)) // same slots deleted in blocks 51-55
		val := []byte{byte(i), 0xEE, 0xFF}
		cs := namedCS(storagePair(addr, slot, val))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	// ── Blocks 86-90: Multiple types in single changeset ────────────
	for i := 86; i <= 90; i++ {
		addr := addrN(byte(i - 80))
		slot := slotN(byte(i))
		cs := namedCS(
			noncePair(addr, uint64(i*9999)),
			storagePair(addr, slot, []byte{byte(i), 0x11, 0x22, 0x33}),
			codePair(addr, []byte{0x60, 0x40, byte(i), byte(i)}),
			codeHashPair(addr, codeHashN(byte(i+50))),
		)
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		commitAndCheck(t, s)
	}
	verifyLtHashAtHeight(t, s, 90)

	// ── Blocks 91-95: Triple ApplyChangeSets per block ──────────────
	// Account gets nonce in call 1, codehash in call 2, storage in call 3.
	for i := 91; i <= 95; i++ {
		addr := addrN(byte(i - 90))

		cs1 := namedCS(noncePair(addr, uint64(i*77777)))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

		cs2 := namedCS(codeHashPair(addr, codeHashN(byte(i+200))))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

		cs3 := namedCS(storagePair(addr, slotN(byte(i+200)), []byte{byte(i)}))
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3}))

		commitAndCheck(t, s)
	}

	// ── Blocks 96-100: Empty blocks and mixed deletes/creates ───────
	// Block 96: empty block
	emptyCS := namedCS()
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{emptyCS}))
	commitAndCheck(t, s)

	// Block 97: delete nonce for addr 15 (sets nonce to 0 but account stays)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(nonceDeletePair(addrN(15))),
	}))
	commitAndCheck(t, s)

	// Block 98: create new account 20 + storage in same changeset
	addr20 := addrN(20)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr20, 1),
			storagePair(addr20, slotN(1), []byte{0x42}),
		),
	}))
	commitAndCheck(t, s)

	// Block 99: update addr 20 nonce + delete its storage in separate calls
	cs99a := namedCS(noncePair(addr20, 2))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs99a}))
	cs99b := namedCS(storageDeletePair(addr20, slotN(1)))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs99b}))
	commitAndCheck(t, s)

	// Block 100: big mixed batch
	var pairs []*iavl.KVPair
	for j := byte(1); j <= 5; j++ {
		pairs = append(pairs, storagePair(addrN(j), slotN(j+250), []byte{j, 0xFF}))
	}
	pairs = append(pairs, noncePair(addrN(20), 999))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)

	// ── Final verification ──────────────────────────────────────────
	verifyLtHashAtHeight(t, s, 100)
}

func verifyLtHashAtHeight(t *testing.T, s *CommitStore, height int64) {
	t.Helper()
	require.Equal(t, height, s.Version(), "unexpected version")

	incremental := s.workingLtHash
	scan := fullScanLtHash(t, s)

	require.True(t, incremental.Equal(scan),
		"LtHash mismatch at height %d:\n  incremental checksum: %x\n  fullscan   checksum: %x",
		height, incremental.Checksum(), scan.Checksum())
}

// TestLtHashNewAccountNoPhantomMixOut is a focused regression test for Bug 1:
// creating a brand new account must NOT MixOut a phantom zero AccountValue.
func TestLtHashNewAccountNoPhantomMixOut(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x42)
	cs := namedCS(noncePair(addr, 1))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	verifyLtHashAtHeight(t, s, 1)
}

// TestLtHashMultiApplyPerBlock is a focused regression test for Bug 2:
// calling ApplyChangeSets twice for the same account in one block must not
// double-MixOut the committed DB value.
func TestLtHashMultiApplyPerBlock(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x77)

	// Block 1: create account
	cs := namedCS(noncePair(addr, 1))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: two separate ApplyChangeSets for same account
	cs1 := namedCS(noncePair(addr, 10))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	ch := codeHashN(0xAB)
	cs2 := namedCS(codeHashPair(addr, ch))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)
}

// TestLtHashStorageAddUpdateDelete verifies that storage operations
// (add → update → delete → re-add) produce correct LtHash at each step.
func TestLtHashStorageAddUpdateDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(1)
	slot := slotN(1)

	// Block 1: add storage
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x11})),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: update storage
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x22})),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Block 3: delete storage
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storageDeletePair(addr, slot)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 3)

	// Block 4: re-add storage at same slot
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x33})),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 4)
}

// TestLtHashCodeAddDelete verifies that code deploy + delete
// produces correct LtHash.
func TestLtHashCodeAddDelete(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(1)

	// Block 1: deploy code + set codehash
	ch := codeHashN(0xAA)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 1),
			codeHashPair(addr, ch),
			codePair(addr, []byte{0x60, 0x80}),
		),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: delete code + clear codehash
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			codeDeletePair(addr),
			codeHashDeletePair(addr),
		),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)
}

// TestLtHashEmptyBlocksNoEffect verifies that empty blocks
// don't corrupt the LtHash.
func TestLtHashEmptyBlocksNoEffect(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(1)

	// Block 1: create some state
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 5),
			storagePair(addr, slotN(1), []byte{0x42}),
		),
	}))
	commitAndCheck(t, s)
	hashAfterBlock1 := s.RootHash()

	// Blocks 2-10: all empty
	for i := 2; i <= 10; i++ {
		require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS()}))
		commitAndCheck(t, s)
	}

	require.Equal(t, hashAfterBlock1, s.RootHash(),
		"empty blocks must not change the root hash")
	verifyLtHashAtHeight(t, s, 10)
}

// TestLtHashSameStorageKeyMultipleTimesInOneChangeset verifies that
// writing the same storage key multiple times in one changeset
// (last-write-wins) produces correct LtHash.
func TestLtHashSameStorageKeyMultipleTimesInOneChangeset(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(1)
	slot := slotN(1)

	// Block 1: write, then overwrite same key in same changeset
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			storagePair(addr, slot, []byte{0x11}),
			storagePair(addr, slot, []byte{0x22}),
		),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)
}

// TestLtHashManyAccountsCreatedAndModified stress-tests the fix by
// creating 50 accounts, then modifying all of them across several blocks.
func TestLtHashManyAccountsCreatedAndModified(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	numAccounts := 50

	// Block 1: create all accounts at once
	var pairs []*iavl.KVPair
	for i := 1; i <= numAccounts; i++ {
		pairs = append(pairs, noncePair(addrN(byte(i)), uint64(i)))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: update all nonces
	pairs = nil
	for i := 1; i <= numAccounts; i++ {
		pairs = append(pairs, noncePair(addrN(byte(i)), uint64(i+1000)))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Block 3: add codehash to first 25 accounts (turns them into contracts)
	pairs = nil
	for i := 1; i <= 25; i++ {
		pairs = append(pairs, codeHashPair(addrN(byte(i)), codeHashN(byte(i))))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 3)

	// Block 4: add storage to all accounts
	pairs = nil
	for i := 1; i <= numAccounts; i++ {
		pairs = append(pairs, storagePair(addrN(byte(i)), slotN(1), []byte{byte(i)}))
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 4)

	// Block 5: delete storage for odd accounts, update for even
	pairs = nil
	for i := 1; i <= numAccounts; i++ {
		if i%2 == 1 {
			pairs = append(pairs, storageDeletePair(addrN(byte(i)), slotN(1)))
		} else {
			pairs = append(pairs, storagePair(addrN(byte(i)), slotN(1), []byte{byte(i), 0xFF}))
		}
	}
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 5)
}

// TestLtHashContractAccountEncodingChange verifies correctness when an
// account transitions between EOA (40 bytes) and contract (72 bytes)
// encoding. This is critical because the encoded length changes.
func TestLtHashContractAccountEncodingChange(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(1)

	// Block 1: create EOA (nonce only → 40 byte encoding)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: add codehash → becomes contract (72 byte encoding)
	ch := codeHashN(0xAB)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeHashPair(addr, ch)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Block 3: clear codehash → back to EOA (40 byte encoding)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(codeHashDeletePair(addr)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 3)
}

// TestLtHashMultipleNamedChangeSetsInOneCall verifies that passing
// multiple NamedChangeSets to a single ApplyChangeSets call works correctly.
func TestLtHashMultipleNamedChangeSetsInOneCall(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(1)

	// Block 1: two NamedChangeSets in one call, modifying same account
	cs1 := namedCS(noncePair(addr, 5))
	cs2 := namedCS(codeHashPair(addr, codeHashN(0xCC)))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1, cs2}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: three NamedChangeSets mixing account + storage + code
	cs3 := namedCS(noncePair(addr, 10))
	cs4 := namedCS(storagePair(addr, slotN(1), []byte{0x42}))
	cs5 := namedCS(codePair(addr, []byte{0x60, 0x80}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs3, cs4, cs5}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)
}

// TestLtHashPersistenceAfterReopen verifies that after reopening the store,
// the persisted LtHash still matches a full scan.
func TestLtHashPersistenceAfterReopen(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: create state and close
	s1 := NewCommitStore(t.Context(), dir, DefaultConfig())
	_, err := s1.LoadVersion(0, false)
	require.NoError(t, err)

	for i := 1; i <= 10; i++ {
		addr := addrN(byte(i))
		cs := namedCS(
			noncePair(addr, uint64(i)),
			storagePair(addr, slotN(byte(i)), []byte{byte(i)}),
		)
		require.NoError(t, s1.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
		_, err := s1.Commit()
		require.NoError(t, err)
	}
	verifyLtHashAtHeight(t, s1, 10)
	require.NoError(t, s1.Close())

	// Phase 2: reopen and verify
	s2 := NewCommitStore(t.Context(), dir, DefaultConfig())
	_, err = s2.LoadVersion(0, false)
	require.NoError(t, err)
	defer s2.Close()

	require.Equal(t, int64(10), s2.Version())
	scan := fullScanLtHash(t, s2)
	require.True(t, s2.workingLtHash.Equal(scan),
		fmt.Sprintf("LtHash mismatch after reopen:\n  persisted checksum: %x\n  fullscan  checksum: %x",
			s2.workingLtHash.Checksum(), scan.Checksum()))
}

// =============================================================================
// fullScanLtHash Includes legacyDB (W-P0-11)
// =============================================================================

func TestFullScanLtHashIncludesLegacy(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := Address{0xAA}
	legacyKey := append([]byte{0x09}, addr[:]...)

	cs := makeChangeSet(legacyKey, []byte{0x42}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs}))
	commitAndCheck(t, s)

	groundTruth := fullScanLtHash(t, s)
	require.Equal(t, s.workingLtHash.Checksum(), groundTruth.Checksum(),
		"full scan including legacyDB should match incremental LtHash")
}

// =============================================================================
// Cross-ApplyChangeSets Same-Key Overwrite LtHash Verification
// =============================================================================

// TestLtHashCrossApplyAccountSameFieldOverwrite verifies that overwriting the
// same account field (nonce→nonce) across two ApplyChangeSets calls in the same
// block produces a correct LtHash. This is distinct from write→delete→write;
// here the second call simply overwrites the pending value.
func TestLtHashCrossApplyAccountSameFieldOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x50)

	// Block 1: create account with nonce=1
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(noncePair(addr, 1)),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: two ApplyChangeSets overwriting the same nonce field
	cs1 := namedCS(noncePair(addr, 10))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	cs2 := namedCS(noncePair(addr, 20))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Verify final value
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, uint64(20), binary.BigEndian.Uint64(val))
}

// TestLtHashCrossApplyStorageOverwrite verifies that overwriting the same
// storage key across two ApplyChangeSets calls in the same block produces
// a correct LtHash (full-scan verified).
func TestLtHashCrossApplyStorageOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x51)
	slot := slotN(0x01)

	// Block 1: create storage entry
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(storagePair(addr, slot, []byte{0x11})),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: overwrite same key in two separate ApplyChangeSets calls
	cs1 := namedCS(storagePair(addr, slot, []byte{0x22}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	cs2 := namedCS(storagePair(addr, slot, []byte{0x33}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Verify final value
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x33}, val)
}

// TestLtHashCrossApplyCodeOverwrite verifies that overwriting the same code
// key across two ApplyChangeSets calls in the same block produces a correct
// LtHash (full-scan verified).
func TestLtHashCrossApplyCodeOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x52)

	// Block 1: deploy code
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 1),
			codePair(addr, []byte{0x60, 0x80}),
			codeHashPair(addr, codeHashN(0xAA)),
		),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: overwrite code in two separate ApplyChangeSets calls
	cs1 := namedCS(codePair(addr, []byte{0x60, 0x40, 0x01}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	cs2 := namedCS(codePair(addr, []byte{0x60, 0x40, 0x02, 0x03}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Verify final value
	key := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	val, found := s.Get(key)
	require.True(t, found)
	require.Equal(t, []byte{0x60, 0x40, 0x02, 0x03}, val)
}

// TestLtHashCrossApplyLegacyOverwrite verifies that overwriting the same
// legacy key across two ApplyChangeSets calls in the same block produces
// a correct LtHash (full-scan verified).
func TestLtHashCrossApplyLegacyOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x53)
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Block 1: create legacy entry
	cs0 := makeChangeSet(legacyKey, []byte{0x00, 0x10}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs0}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: overwrite same legacy key in two separate ApplyChangeSets calls
	cs1 := makeChangeSet(legacyKey, []byte{0x00, 0x20}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1}))

	cs2 := makeChangeSet(legacyKey, []byte{0x00, 0x30}, false)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2}))

	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Verify final value
	val, found := s.Get(legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x30}, val)
}

// TestLtHashCrossApplyMixedOverwrite is a comprehensive test that exercises
// cross-Apply overwrites for ALL key types simultaneously in the same block,
// verifying that the incremental LtHash remains correct.
func TestLtHashCrossApplyMixedOverwrite(t *testing.T) {
	s := setupTestStore(t)
	defer s.Close()

	addr := addrN(0x54)
	slot := slotN(0x01)
	legacyKey := append([]byte{0x09}, addr[:]...)

	// Block 1: create initial state for all key types
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 1),
			codeHashPair(addr, codeHashN(0x10)),
			codePair(addr, []byte{0x60, 0x80}),
			storagePair(addr, slot, []byte{0x11}),
		),
	}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		makeChangeSet(legacyKey, []byte{0x00, 0x01}, false),
	}))
	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 1)

	// Block 2: first Apply — update all key types
	cs1a := namedCS(
		noncePair(addr, 10),
		codeHashPair(addr, codeHashN(0x20)),
		codePair(addr, []byte{0x60, 0x40}),
		storagePair(addr, slot, []byte{0x22}),
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs1a}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		makeChangeSet(legacyKey, []byte{0x00, 0x02}, false),
	}))

	// Block 2: second Apply — overwrite all key types again
	cs2a := namedCS(
		noncePair(addr, 100),
		codeHashPair(addr, codeHashN(0x30)),
		codePair(addr, []byte{0x60, 0x60, 0x01}),
		storagePair(addr, slot, []byte{0x33}),
	)
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{cs2a}))
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{
		makeChangeSet(legacyKey, []byte{0x00, 0x03}, false),
	}))

	commitAndCheck(t, s)
	verifyLtHashAtHeight(t, s, 2)

	// Verify all final values
	nonceKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, addr[:])
	nonceVal, found := s.Get(nonceKey)
	require.True(t, found)
	require.Equal(t, uint64(100), binary.BigEndian.Uint64(nonceVal))

	chKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCodeHash, addr[:])
	chVal, found := s.Get(chKey)
	require.True(t, found)
	expected := codeHashN(0x30)
	require.Equal(t, expected[:], chVal)

	codeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr[:])
	codeVal, found := s.Get(codeKey)
	require.True(t, found)
	require.Equal(t, []byte{0x60, 0x60, 0x01}, codeVal)

	storageKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, StorageKey(addr, slot))
	storageVal, found := s.Get(storageKey)
	require.True(t, found)
	require.Equal(t, []byte{0x33}, storageVal)

	legacyVal, found := s.Get(legacyKey)
	require.True(t, found)
	require.Equal(t, []byte{0x00, 0x03}, legacyVal)
}
