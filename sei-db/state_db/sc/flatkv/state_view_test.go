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

// A store with no sealed block has no view to hand out, and says so. Teardown clears the holder, so
// without an answer here the read path would dereference nil and report nothing about what went wrong.
func TestOpenViewOnClosedStoreReportsTheStoreIsNotOpen(t *testing.T) {
	s := setupTestStore(t)
	commitNonce(t, s, 1, addrN(1), 7)
	require.NoError(t, s.Close())

	require.PanicsWithValue(t,
		"flatkv: OpenView: no sealed block: the store is not open",
		func() { s.OpenView() })
}

// A second Close must not hand back a reservation this view no longer owns. The damaging case is
// silent: while the store still has the same block installed, the extra release takes that
// reservation's count to zero with no error reported, retiring a view the store believes is live. The
// commits below are what surface it — sealing the next block reserves the installed view, which now
// fails as retired.
func TestOpenViewCloseIsIdempotent(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	stateView := s.OpenView()
	require.Equal(t, uint64(7), stateView.GetNonce(gigaAddr(addr)))
	stateView.Close()
	stateView.Close()

	commitNonce(t, s, 2, addr, 8)
	commitNonce(t, s, 3, addr, 9)
	requireFlushedToDisk(t, s)

	latest := s.OpenView()
	defer latest.Close()
	require.Equal(t, uint64(9), latest.GetNonce(gigaAddr(addr)))
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
		require.Equal(t, giga.EmptyCodeHash, stateView.GetCodeHash(addr),
			"an account that exists with no code hashes as keccak256(\"\"), not as zero")
		require.Nil(t, stateView.GetCode(addr))
		require.Zero(t, stateView.GetCodeSize(addr))
	})

	t.Run("account that does not exist", func(t *testing.T) {
		addr := gigaAddr(missing)
		require.False(t, stateView.AccountExists(addr))
		require.Zero(t, stateView.GetNonce(addr))
		require.Equal(t, giga.Hash{}, stateView.GetCodeHash(addr),
			"an account that does not exist hashes as zero, not as keccak256(\"\")")
		require.Nil(t, stateView.GetCode(addr))
		require.Zero(t, stateView.GetCodeSize(addr))
		require.Equal(t, giga.Hash{}, stateView.GetStorage(addr, giga.Hash(slot)))
	})
}

// Balance has no key kind yet, so nothing can write one (store_apply.go passes nil balance changes).
// Refusing is the only honest answer: zero would be indistinguishable from a real zero balance, and
// the caller has no way to tell the two apart. The account below has a nonce, so its row does exist.
func TestStateViewBalancePanicsUntilWritable(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	commitNonce(t, s, 1, addr, 7)

	stateView := s.OpenView()
	defer stateView.Close()

	require.PanicsWithValue(t,
		"flatkv: GetBalance is unimplemented; FlatKV does not store balances",
		func() { stateView.GetBalance(gigaAddr(addr)) })
}

// Get answers with the value alone. Each row is stored as version||blockHeight||value, so returning
// the row would hand every caller the height the key was last modified at — a separate question with
// no accessor here. Each case therefore asserts the exact bytes, length included, since a header
// creeping back in would still deserialize.
func TestStateViewGetReturnsValues(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	addr := addrN(1)
	eoa := addrN(2)
	slot := slotN(1)
	bytecode := []byte{0x60, 0x80}
	codeHash := codeHashN(0xAB)

	require.NoError(t, s.CommitStateChanges(1, []*proto.NamedChangeSet{
		namedCS(
			noncePair(addr, 5),
			codeHashPair(addr, codeHash),
			codePair(addr, bytecode),
			storagePair(addr, slot, []byte{0xEE}),
			noncePair(eoa, 9),
		),
		{
			Name:      "bank",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("balances/x"), Value: []byte("v")}}},
		},
	}))

	stateView := s.OpenView()
	defer stateView.Close()

	t.Run("nonce", func(t *testing.T) {
		value, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]))
		require.True(t, found)
		require.Len(t, value, vtype.NonceLen, "a nonce is 8 bytes; anything longer is row header")
		require.Equal(t, uint64(5), binary.BigEndian.Uint64(value))
	})

	t.Run("code hash", func(t *testing.T) {
		value, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]))
		require.True(t, found)
		require.Equal(t, codeHash[:], value,
			"a code-hash key reads the account row but answers with that one field")
	})

	t.Run("storage", func(t *testing.T) {
		value, found := stateView.Get(keys.EVMStoreKey,
			keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)))
		require.True(t, found)
		require.Equal(t, padLeft32(0xEE), value)
	})

	t.Run("code", func(t *testing.T) {
		value, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr[:]))
		require.True(t, found)
		require.Equal(t, bytecode, value)
	})

	t.Run("non-EVM module", func(t *testing.T) {
		value, found := stateView.Get("bank", []byte("balances/x"))
		require.True(t, found)
		require.Equal(t, []byte("v"), value,
			"a one-byte value must arrive one byte long, not with a header in front of it")
	})

	t.Run("absent key", func(t *testing.T) {
		absent := addrN(9)
		_, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, absent[:]))
		require.False(t, found)
	})

	t.Run("code hash of an account with no code", func(t *testing.T) {
		_, found := stateView.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, eoa[:]))
		require.False(t, found,
			"Get reports what is stored; substituting EmptyCodeHash here is GetCodeHash's job")
	})
}
