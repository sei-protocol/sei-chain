package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

const importBlockHeight = int64(42)

func findPair(t *testing.T, pairs []PhysicalKVPair, key []byte) PhysicalKVPair {
	t.Helper()
	for _, p := range pairs {
		if string(p.Key) == string(key) {
			return p
		}
	}
	t.Fatalf("pair with key %x not found", key)
	return PhysicalKVPair{}
}

func TestImportTranslator_NilOrEmptyChangeSet(t *testing.T) {
	tr := NewImportTranslator(importBlockHeight)

	pairs, err := tr.Translate(nil)
	require.NoError(t, err)
	require.Empty(t, pairs)

	emptyCS := &proto.NamedChangeSet{Name: keys.EVMStoreKey}
	pairs, err = tr.Translate(emptyCS)
	require.NoError(t, err)
	require.Empty(t, pairs)

	require.Empty(t, tr.Finalize())
}

func TestImportTranslator_StorageEntry(t *testing.T) {
	addr := addrN(0x42)
	slot := slotN(0x07)
	val := padLeft32(0x2A)

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(storagePair(addr, slot, []byte{0x2A})))
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	expectedKey := storagePhysKey(addr, slot)
	require.Equal(t, expectedKey, pairs[0].Key)

	got, err := vtype.DeserializeStorageData(pairs[0].Value)
	require.NoError(t, err)
	require.Equal(t, importBlockHeight, got.GetBlockHeight())
	require.Equal(t, val, got.GetValue()[:])
	require.False(t, got.IsDelete())

	require.Empty(t, tr.Finalize())
}

func TestImportTranslator_CodeEntry(t *testing.T) {
	addr := addrN(0x42)
	bytecode := []byte{0x60, 0x2A, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xF3}

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(codePair(addr, bytecode)))
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	expectedKey := ktype.EVMPhysicalKey(keys.EVMKeyCode, addr[:])
	require.Equal(t, expectedKey, pairs[0].Key)

	got, err := vtype.DeserializeCodeData(pairs[0].Value)
	require.NoError(t, err)
	require.Equal(t, importBlockHeight, got.GetBlockHeight())
	require.Equal(t, bytecode, got.GetBytecode())

	require.Empty(t, tr.Finalize())
}

func TestImportTranslator_MiscEntryWithinEVMModule(t *testing.T) {
	addr := addrN(0x42)
	rawKey := append([]byte{0x09}, addr[:]...)
	rawValue := []byte{0xAA, 0xBB}

	tr := NewImportTranslator(importBlockHeight)
	cs := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: rawKey, Value: rawValue},
		}},
	}
	pairs, err := tr.Translate(cs)
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	expectedKey := ktype.ModulePhysicalKey(keys.EVMStoreKey, rawKey)
	require.Equal(t, expectedKey, pairs[0].Key)

	got, err := vtype.DeserializeMiscData(pairs[0].Value)
	require.NoError(t, err)
	require.Equal(t, importBlockHeight, got.GetBlockHeight())
	require.Equal(t, rawValue, got.GetValue())
	require.False(t, got.IsDelete())

	require.Empty(t, tr.Finalize())
}

func TestImportTranslator_NonEVMModuleRoutesToMisc(t *testing.T) {
	rawKey := []byte("custom-key")
	rawValue := []byte("custom-value")

	tr := NewImportTranslator(importBlockHeight)
	cs := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: rawKey, Value: rawValue},
		}},
	}
	pairs, err := tr.Translate(cs)
	require.NoError(t, err)
	require.Len(t, pairs, 1)

	expectedKey := ktype.ModulePhysicalKey("bank", rawKey)
	require.Equal(t, expectedKey, pairs[0].Key)

	got, err := vtype.DeserializeMiscData(pairs[0].Value)
	require.NoError(t, err)
	require.Equal(t, rawValue, got.GetValue())

	require.Empty(t, tr.Finalize())
}

func TestImportTranslator_NonceOnlyAccountEmittedByFinalize(t *testing.T) {
	addr := addrN(0x42)

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(noncePair(addr, 7)))
	require.NoError(t, err)
	require.Empty(t, pairs, "account fragments must be buffered, not emitted by Translate")

	finalized := tr.Finalize()
	require.Len(t, finalized, 1)

	expectedKey := accountPhysKey(addr)
	require.Equal(t, expectedKey, finalized[0].Key)

	got, err := vtype.DeserializeAccountData(finalized[0].Value)
	require.NoError(t, err)
	require.Equal(t, uint64(7), got.GetNonce())
	require.Equal(t, importBlockHeight, got.GetBlockHeight())

	var zero vtype.CodeHash
	require.Equal(t, zero, *got.GetCodeHash(), "code hash must default to zero for EOA")
}

func TestImportTranslator_CodeHashOnlyAccountEmittedByFinalize(t *testing.T) {
	addr := addrN(0x44)
	ch := codeHashN(0xCD)

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(codeHashPair(addr, ch)))
	require.NoError(t, err)
	require.Empty(t, pairs)

	finalized := tr.Finalize()
	require.Len(t, finalized, 1)

	got, err := vtype.DeserializeAccountData(finalized[0].Value)
	require.NoError(t, err)
	require.Equal(t, ch, *got.GetCodeHash())
	require.Equal(t, uint64(0), got.GetNonce(), "nonce must default to zero")
}

func TestImportTranslator_NonceAndCodeHashSameCallMerge(t *testing.T) {
	addr := addrN(0x42)
	ch := codeHashN(0xAB)

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(
		noncePair(addr, 9),
		codeHashPair(addr, ch),
	))
	require.NoError(t, err)
	require.Empty(t, pairs)

	finalized := tr.Finalize()
	require.Len(t, finalized, 1)

	got, err := vtype.DeserializeAccountData(finalized[0].Value)
	require.NoError(t, err)
	require.Equal(t, uint64(9), got.GetNonce())
	require.Equal(t, ch, *got.GetCodeHash())
}

func TestImportTranslator_NonceAndCodeHashCrossCallMerge(t *testing.T) {
	addr := addrN(0x42)
	ch := codeHashN(0xAB)

	tr := NewImportTranslator(importBlockHeight)
	_, err := tr.Translate(namedCS(noncePair(addr, 9)))
	require.NoError(t, err)

	_, err = tr.Translate(namedCS(codeHashPair(addr, ch)))
	require.NoError(t, err)

	finalized := tr.Finalize()
	require.Len(t, finalized, 1, "fragments split across calls must merge into one account")

	got, err := vtype.DeserializeAccountData(finalized[0].Value)
	require.NoError(t, err)
	require.Equal(t, uint64(9), got.GetNonce())
	require.Equal(t, ch, *got.GetCodeHash())
}

func TestImportTranslator_DropsDeletes(t *testing.T) {
	addr := addrN(0x42)
	slot := slotN(0x01)

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(
		storageDeletePair(addr, slot),
		codeDeletePair(addr),
		nonceDeletePair(addr),
		codeHashDeletePair(addr),
	))
	require.NoError(t, err)
	require.Empty(t, pairs)
	require.Empty(t, tr.Finalize(), "deletes must not produce any account either")
}

func TestImportTranslator_RejectsEmptyKey(t *testing.T) {
	tr := NewImportTranslator(importBlockHeight)
	cs := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: nil, Value: []byte{0x01}},
		}},
	}
	_, err := tr.Translate(cs)
	require.Error(t, err)
}

func TestImportTranslator_RejectsInvalidNonce(t *testing.T) {
	addr := addrN(0x42)
	tr := NewImportTranslator(importBlockHeight)
	cs := &proto.NamedChangeSet{
		Name: keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: []byte{0x01, 0x02}},
		}},
	}
	_, err := tr.Translate(cs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonce")
}

func TestImportTranslator_StorageAndAccountInOneCall(t *testing.T) {
	addr := addrN(0x42)
	slot := slotN(0x07)
	ch := codeHashN(0xAB)

	tr := NewImportTranslator(importBlockHeight)
	pairs, err := tr.Translate(namedCS(
		storagePair(addr, slot, []byte{0x2A}),
		noncePair(addr, 7),
		codeHashPair(addr, ch),
	))
	require.NoError(t, err)
	require.Len(t, pairs, 1, "storage emitted immediately; account fragments buffered")

	storagePair := findPair(t, pairs, storagePhysKey(addr, slot))
	storageGot, err := vtype.DeserializeStorageData(storagePair.Value)
	require.NoError(t, err)
	require.Equal(t, padLeft32(0x2A), storageGot.GetValue()[:])

	finalized := tr.Finalize()
	require.Len(t, finalized, 1)
	acctGot, err := vtype.DeserializeAccountData(finalized[0].Value)
	require.NoError(t, err)
	require.Equal(t, uint64(7), acctGot.GetNonce())
	require.Equal(t, ch, *acctGot.GetCodeHash())
}

func TestImportTranslator_FinalizeClearsBuffer(t *testing.T) {
	addr := addrN(0x42)
	tr := NewImportTranslator(importBlockHeight)
	_, err := tr.Translate(namedCS(noncePair(addr, 1)))
	require.NoError(t, err)

	first := tr.Finalize()
	require.Len(t, first, 1)
	second := tr.Finalize()
	require.Empty(t, second, "Finalize must be idempotent on an exhausted translator")
}

// TestImportTranslator_TranslateAfterFinalizeReturnsError locks the
// single-shot contract: any Translate call that happens after Finalize
// has cleared the pending-account buffer must surface
// ErrImportTranslatorFinalized rather than panic on the nil map. The
// existing CLI never calls in this order, so this is a defensive
// regression knob: if a future caller (or refactor) introduces a
// post-Finalize call, the failure is explicit and recoverable instead
// of a runtime panic deep inside the merge path.
func TestImportTranslator_TranslateAfterFinalizeReturnsError(t *testing.T) {
	tr := NewImportTranslator(importBlockHeight)
	_ = tr.Finalize()

	out, err := tr.Translate(namedCS(noncePair(addrN(0x42), 1)))
	require.ErrorIs(t, err, ErrImportTranslatorFinalized)
	require.Nil(t, out)
}
