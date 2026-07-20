package operations

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

// TestClassifyFlatKVPhysicalKey verifies the logical-DB bucket assignment
// used by the state-size breakdown. We build physical keys with the same
// helpers FlatKV uses internally so the test tracks the live encoding.
func TestClassifyFlatKVPhysicalKey(t *testing.T) {
	addr := ktype.Address{0xAA}
	slot := ktype.Slot{0xBB}

	cases := []struct {
		name     string
		key      []byte
		expected string
	}{
		{
			name:     "evm nonce -> account",
			key:      ktype.EVMPhysicalKey(keys.EVMKeyNonce, addr[:]),
			expected: "account",
		},
		{
			name:     "evm codehash -> account (merged)",
			key:      ktype.EVMPhysicalKey(keys.EVMKeyCodeHash, addr[:]),
			expected: "account",
		},
		{
			name:     "evm code -> code",
			key:      ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:]),
			expected: "code",
		},
		{
			name:     "evm storage -> storage",
			key:      ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
			expected: "storage",
		},
		{
			name:     "non-evm module -> legacy",
			key:      ktype.ModulePhysicalKey("bank", []byte("supply")),
			expected: "legacy",
		},
		{
			name:     "evm with unknown prefix byte -> legacy",
			key:      append([]byte("evm/"), 0xFF, 0xAA),
			expected: "legacy",
		},
		{
			name:     "empty evm inner key -> legacy",
			key:      []byte("evm/"),
			expected: "legacy",
		},
		{
			name:     "missing module prefix -> legacy",
			key:      []byte{0x03, 0x04, 0x05},
			expected: "legacy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, classifyFlatKVPhysicalKey(tc.key))
		})
	}
}

// TestExtractFlatKVContractAddress confirms the 20-byte address after the
// 0x03 storage prefix is surfaced as an uppercase hex string, matching the
// output format used by ContractSizeEntry across memIAVL and FlatKV.
func TestExtractFlatKVContractAddress(t *testing.T) {
	addr := ktype.Address{0xDE, 0xAD, 0xBE, 0xEF}
	slot := ktype.Slot{0x01}
	physKey := ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))

	got := extractFlatKVContractAddress(physKey)
	want := "DEADBEEF" + strings.Repeat("00", 16)
	require.Equal(t, want, got)

	// Non-evm keys (no address component) should return empty so the caller
	// can skip ContractSizes bookkeeping.
	require.Empty(t, extractFlatKVContractAddress(ktype.ModulePhysicalKey("bank", []byte("s"))))

	// Too-short inner key guard.
	require.Empty(t, extractFlatKVContractAddress([]byte("evm/\x03short")))
}

// TestCollectFlatKVStateSize exercises the full scan path against a real
// in-memory FlatKV store. We seed a mix of account, code, storage and
// legacy rows, then verify per-DB counts and the top-contracts ranking by
// storage size.
func TestCollectFlatKVStateSize(t *testing.T) {
	store := newTestFlatKVStore(t)
	defer func() { require.NoError(t, store.Close()) }()

	addrA := addrN(0x11)
	addrB := addrN(0x22)
	addrC := addrN(0x33)

	// Write nonces for three addrs, codehashes for two (codehash and
	// nonce canonicalise to the same 0x0a account row per address, so
	// the account DB ends up with three rows keyed by addrA/B/C).
	pairs := []*proto.KVPair{
		noncePair(addrA, 1),
		noncePair(addrB, 2),
		noncePair(addrC, 3),
		codeHashPair(addrA, codeHashOf(0xAA)),
		codeHashPair(addrB, codeHashOf(0xBB)),

		// Two code rows (addrA, addrB).
		codePair(addrA, []byte{0x60, 0x80, 0x60, 0x40}),
		codePair(addrB, []byte{0x60, 0x80, 0x60, 0x40, 0x52, 0x34}),

		// addrA: 3 storage slots; addrB: 5 storage slots.
		// addrB must outrank addrA in the top-contracts table.
		storagePair(addrA, slotN(0x01), 0x01),
		storagePair(addrA, slotN(0x02), 0x02),
		storagePair(addrA, slotN(0x03), 0x03),
		storagePair(addrB, slotN(0x01), 0xAA),
		storagePair(addrB, slotN(0x02), 0xBB),
		storagePair(addrB, slotN(0x03), 0xCC),
		storagePair(addrB, slotN(0x04), 0xDD),
		storagePair(addrB, slotN(0x05), 0xEE),
	}
	evmCS := &proto.NamedChangeSet{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}

	// One non-evm write goes to the legacy DB.
	bankCS := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("supply"), Value: []byte("100")},
		}},
	}

	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{evmCS, bankCS}))
	_, err := store.Commit()
	require.NoError(t, err)

	result, err := collectFlatKVStateSize(store)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Per-DB counts.
	require.NotNil(t, result.DBSizes["account"], "expected account DB breakdown")
	require.EqualValues(t, 3, result.DBSizes["account"].NumKeys,
		"nonce+codehash for same address merge to one account row")

	require.NotNil(t, result.DBSizes["code"], "expected code DB breakdown")
	require.EqualValues(t, 2, result.DBSizes["code"].NumKeys)

	require.NotNil(t, result.DBSizes["storage"], "expected storage DB breakdown")
	require.EqualValues(t, 8, result.DBSizes["storage"].NumKeys,
		"3 slots for addrA + 5 slots for addrB = 8 physical storage rows")

	require.NotNil(t, result.DBSizes["legacy"], "expected legacy DB breakdown")
	require.EqualValues(t, 1, result.DBSizes["legacy"].NumKeys,
		"one non-evm bank row should land in legacy")

	// Total key count is the sum of per-DB counts.
	require.EqualValues(t, 3+2+8+1, result.Total.NumKeys)

	// Size accounting is internally consistent.
	require.Equal(t, result.Total.KeySize+result.Total.ValueSize, result.Total.TotalSize)

	// Top contracts: exactly two entries, addrB should outrank addrA.
	require.Len(t, result.ContractSizes, 2)
	hexA := addrHex(addrA)
	hexB := addrHex(addrB)
	require.Contains(t, result.ContractSizes, hexA)
	require.Contains(t, result.ContractSizes, hexB)
	require.Greater(t,
		result.ContractSizes[hexB].TotalSize,
		result.ContractSizes[hexA].TotalSize,
		"addrB has more storage rows so it must rank higher",
	)
	require.EqualValues(t, 3, result.ContractSizes[hexA].KeyCount)
	require.EqualValues(t, 5, result.ContractSizes[hexB].KeyCount)
}

// TestFlatKVStateSizeAnalysisShape verifies the StateSizeAnalysis row the
// tool emits for DynamoDB export is well-formed: correct module name, sane
// totals, and JSON-encoded prefix/contract breakdowns that downstream
// consumers can parse.
func TestFlatKVStateSizeAnalysisShape(t *testing.T) {
	r := &FlatKVStateSizeResult{
		Total: FlatKVDBSize{NumKeys: 10, KeySize: 100, ValueSize: 200, TotalSize: 300},
		DBSizes: map[string]*FlatKVDBSize{
			"account": {NumKeys: 2, KeySize: 30, ValueSize: 70, TotalSize: 100},
			"storage": {NumKeys: 8, KeySize: 70, ValueSize: 130, TotalSize: 200},
		},
	}
	analysis := flatkvStateSizeAnalysis(r, 42)

	require.Equal(t, flatkvAnalysisModuleName, analysis.ModuleName)
	require.EqualValues(t, 42, analysis.BlockHeight)
	require.EqualValues(t, 10, analysis.TotalNumKeys)
	require.EqualValues(t, 300, analysis.TotalSize)

	require.Contains(t, analysis.PrefixBreakdown, `"account"`)
	require.Contains(t, analysis.PrefixBreakdown, `"storage"`)
	require.Equal(t, "[]", analysis.ContractBreakdown,
		"empty ContractSizes must still marshal to a valid empty array")
}

// -------- test-only helpers (small duplicates of internal flatkv helpers) --------

func newTestFlatKVStore(t *testing.T) *flatkv.CommitStore {
	t.Helper()
	s, err := flatkv.NewCommitStore(context.Background(), flatkvconfig.DefaultTestConfig(t))
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	return s
}

func addrN(last byte) ktype.Address {
	var a ktype.Address
	a[19] = last
	return a
}

func slotN(last byte) ktype.Slot {
	var s ktype.Slot
	s[31] = last
	return s
}

func codeHashOf(b byte) vtype.CodeHash {
	var h vtype.CodeHash
	for i := range h {
		h[i] = b
	}
	return h
}

func nonceBytesBE(n uint64) []byte {
	b := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func padLeft32(val byte) []byte {
	var b [32]byte
	b[31] = val
	return b[:]
}

func noncePair(addr ktype.Address, nonce uint64) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		Value: nonceBytesBE(nonce),
	}
}

func codeHashPair(addr ktype.Address, ch vtype.CodeHash) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
		Value: ch[:],
	}
}

func codePair(addr ktype.Address, bytecode []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
		Value: bytecode,
	}
}

func storagePair(addr ktype.Address, slot ktype.Slot, val byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
		Value: padLeft32(val),
	}
}

func addrHex(a ktype.Address) string {
	const hex = "0123456789ABCDEF"
	b := make([]byte, 2*len(a))
	for i, v := range a {
		b[2*i] = hex[v>>4]
		b[2*i+1] = hex[v&0x0F]
	}
	return string(b)
}
