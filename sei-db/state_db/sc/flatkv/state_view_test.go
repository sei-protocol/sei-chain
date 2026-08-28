package flatkv

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// gigaAddr converts a test address to the Giga API's address type. Both are [20]byte.
func gigaAddr(addr ktype.Address) giga.Address {
	return giga.Address(addr)
}

// commitNonce writes addr's nonce as a whole block through the Giga write entry point.
func commitNonce(t *testing.T, s *CommitStore, version int64, addr ktype.Address, nonce uint64) {
	t.Helper()
	require.NoError(t, s.CommitStateChanges(version, []*proto.NamedChangeSet{namedCS(noncePair(addr, nonce))}))
}

// CommitStateChanges is the Giga write entry point, and its whole job is that a block committed
// through it is committed — visible to the reads that follow.
func TestCommitStateChangesCommitsBlock(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	require.Equal(t, int64(1), s.Version())
	value, found := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
	require.True(t, found)
	require.Equal(t, nonceBytes(7), value)
}

// Blocks are contiguous, and committing through the Giga entry point does not relax that.
func TestCommitStateChangesRejectsNonContiguousVersion(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	err := s.CommitStateChanges(5, []*proto.NamedChangeSet{namedCS(noncePair(addrN(1), 7))})
	require.Error(t, err)
	require.Equal(t, int64(0), s.Version(), "a refused block must not advance the store")
}

// OpenView serves the block most recently committed, and says which block that is.
func TestOpenViewReadsCommittedBlock(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	stateView := s.OpenView()
	defer stateView.Close()

	require.Equal(t, int64(1), stateView.GetBlockHeight())
	require.Equal(t, uint64(7), stateView.GetNonce(gigaAddr(addr)))
}

// The point of a view is that it is pinned: it keeps answering for the block it was opened on however
// many blocks are committed over the top of it.
func TestOpenViewIsIsolatedFromLaterCommits(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	stateView := s.OpenView()
	defer stateView.Close()

	for version := int64(2); version <= 4; version++ {
		commitNonce(t, s, version, addr, uint64(version)+10)
	}

	require.Equal(t, int64(1), stateView.GetBlockHeight(), "a view must not follow the store forward")
	require.Equal(t, uint64(7), stateView.GetNonce(gigaAddr(addr)),
		"a view must not observe writes committed after it was opened")

	latest := s.OpenView()
	defer latest.Close()
	require.Equal(t, int64(4), latest.GetBlockHeight())
	require.Equal(t, uint64(14), latest.GetNonce(gigaAddr(addr)))
}

// Every OpenView takes a reservation that only Close hands back, and an unreleased view stalls its
// store's flushes forever. So a leak here does not fail an assertion — it hangs the flush below.
func TestOpenViewCloseReturnsReservation(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	for i := 0; i < 50; i++ {
		stateView := s.OpenView()
		require.Equal(t, uint64(7), stateView.GetNonce(gigaAddr(addr)))
		stateView.Close()
	}

	commitNonce(t, s, 2, addr, 8)
	requireFlushedToDisk(t, s)
}

// The EVM accessors are the read surface the executor actually uses, so each one is pinned against a
// present account, and against an address that was never written.
func TestStateViewEVMAccessors(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	contract, eoa, missing := addrN(1), addrN(2), addrN(3)
	slot := slotN(1)
	bytecode := []byte{0x60, 0x80, 0x60, 0x40}
	codeHash := codeHashN(0xAB)

	require.NoError(t, s.CommitStateChanges(1, []*proto.NamedChangeSet{namedCS(
		noncePair(contract, 3),
		codeHashPair(contract, codeHash),
		codePair(contract, bytecode),
		storagePair(contract, slot, []byte{0xEE}),
		noncePair(eoa, 9),
	)}))

	stateView := s.OpenView()
	defer stateView.Close()

	t.Run("contract", func(t *testing.T) {
		addr := gigaAddr(contract)
		require.True(t, stateView.AccountExists(addr))
		require.Equal(t, uint64(3), stateView.GetNonce(addr))
		require.Equal(t, giga.Hash(codeHash), stateView.GetCodeHash(addr))
		require.Equal(t, bytecode, stateView.GetCode(addr))
		require.Equal(t, len(bytecode), stateView.GetCodeSize(addr))
		require.Equal(t, giga.Hash(padLeft32(0xEE)[0:32]), stateView.GetStorage(addr, giga.Hash(slot)))
		require.Equal(t, giga.Hash{}, stateView.GetStorage(addr, giga.Hash(slotN(9))),
			"an unset slot reads as zero, not as missing")
	})

	t.Run("account without code", func(t *testing.T) {
		addr := gigaAddr(eoa)
		require.True(t, stateView.AccountExists(addr))
		require.Equal(t, uint64(9), stateView.GetNonce(addr))
		require.Equal(t, giga.Hash{}, stateView.GetCodeHash(addr),
			"nothing was stored, so nothing is reported")
		require.Nil(t, stateView.GetCode(addr))
		require.Zero(t, stateView.GetCodeSize(addr))
	})

	t.Run("account that does not exist", func(t *testing.T) {
		addr := gigaAddr(missing)
		require.False(t, stateView.AccountExists(addr))
		require.Zero(t, stateView.GetNonce(addr))
		require.Equal(t, giga.Hash{}, stateView.GetCodeHash(addr))
		require.Nil(t, stateView.GetCode(addr))
		require.Zero(t, stateView.GetCodeSize(addr))
		require.Equal(t, giga.Hash{}, stateView.GetStorage(addr, giga.Hash(slot)))
	})
}

// The view is an alternate way to read data FlatKV already serves, so every accessor must answer what
// CommitStore.Get answers for the same key at the same height. This walks addresses covering a
// contract, an account with no code, and one that was never written, and holds the two paths against
// each other rather than against hand-written expectations.
func TestStateViewAgreesWithStoreReads(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	contract, eoa, missing := addrN(1), addrN(2), addrN(3)
	setSlot, unsetSlot := slotN(1), slotN(9)

	require.NoError(t, s.CommitStateChanges(1, []*proto.NamedChangeSet{namedCS(
		noncePair(contract, 3),
		codeHashPair(contract, codeHashN(0xAB)),
		codePair(contract, []byte{0x60, 0x80, 0x60, 0x40}),
		storagePair(contract, setSlot, []byte{0xEE}),
		noncePair(eoa, 9),
	)}))

	stateView := s.OpenView()
	defer stateView.Close()

	for _, addr := range []ktype.Address{contract, eoa, missing} {
		for _, slot := range []ktype.Slot{setSlot, unsetSlot} {
			raw, found := s.Get(keys.EVMStoreKey,
				keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
			require.Equal(t, hashOrZero(raw, found), stateView.GetStorage(gigaAddr(addr), giga.Hash(slot)),
				"storage %x/%x", addr, slot)
		}

		raw, found := s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
		var wantNonce uint64
		if found {
			wantNonce = binary.BigEndian.Uint64(raw)
		}
		require.Equal(t, wantNonce, stateView.GetNonce(gigaAddr(addr)), "nonce %x", addr)

		raw, found = s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
		require.Equal(t, hashOrZero(raw, found), stateView.GetCodeHash(gigaAddr(addr)), "code hash %x", addr)

		raw, found = s.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
		if !found {
			raw = nil
		}
		require.Equal(t, raw, stateView.GetCode(gigaAddr(addr)), "code %x", addr)
		require.Equal(t, len(raw), stateView.GetCodeSize(gigaAddr(addr)), "code size %x", addr)
	}
}

// hashOrZero reads a CommitStore.Get result as the hash the view reports for the same key.
func hashOrZero(raw []byte, found bool) giga.Hash {
	if !found {
		return giga.Hash{}
	}
	return giga.Hash(raw)
}

// Balance has no key kind yet, so nothing can write one (store_apply.go passes nil balance changes).
// The accessor still has to answer, and zero is the answer until that lands.
func TestStateViewBalanceIsZeroUntilWritable(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	stateView := s.OpenView()
	defer stateView.Close()

	require.Equal(t, giga.Hash{}, stateView.GetBalance(gigaAddr(addr)))
}

// Get hands back whole rows and interprets none of them. That is the whole reason it exists alongside
// the EVM accessors, so each row type is checked to deserialize into the record that was written —
// not into the one field a caller happened to ask for.
func TestStateViewGetReturnsWholeRows(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	slot := slotN(1)
	bytecode := []byte{0x60, 0x80}
	codeHash := codeHashN(0xAB)

	require.NoError(t, s.CommitStateChanges(1, []*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 5),
			codeHashPair(addr, codeHash),
			codePair(addr, bytecode),
			storagePair(addr, slot, []byte{0xEE}),
		),
		{
			Name:      "bank",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("balances/x"), Value: []byte("v")}}},
		},
	}))

	stateView := s.OpenView()
	defer stateView.Close()

	t.Run("account row carries every field", func(t *testing.T) {
		raw, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
		require.True(t, found)

		account, err := vtype.DeserializeAccountData(raw)
		require.NoError(t, err)
		require.Equal(t, uint64(5), account.GetNonce())
		require.Equal(t, codeHash, *account.GetCodeHash(),
			"the row must arrive whole; a nonce key is not a request for the nonce field alone")
	})

	t.Run("storage row", func(t *testing.T) {
		raw, found := stateView.Get(keys.EVMStoreKey,
			keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
		require.True(t, found)

		storage, err := vtype.DeserializeStorageData(raw)
		require.NoError(t, err)
		require.Equal(t, padLeft32(0xEE), storage.GetValue()[:])
	})

	t.Run("code row", func(t *testing.T) {
		raw, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
		require.True(t, found)

		code, err := vtype.DeserializeCodeData(raw)
		require.NoError(t, err)
		require.Equal(t, bytecode, code.GetBytecode())
	})

	t.Run("non-EVM module", func(t *testing.T) {
		raw, found := stateView.Get("bank", []byte("balances/x"))
		require.True(t, found)

		misc, err := vtype.DeserializeMiscData(raw)
		require.NoError(t, err)
		require.Equal(t, []byte("v"), misc.GetValue())
	})

	t.Run("absent key", func(t *testing.T) {
		absent := addrN(9)
		_, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, absent[:]))
		require.False(t, found)
	})

	t.Run("code hash key is refused", func(t *testing.T) {
		// FlatKV keeps nonce, balance and code hash in one row, so a code hash key names a field
		// rather than a row. Serving it would make Get a field accessor by the back door.
		require.Panics(t, func() {
			stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
		})
	})
}
