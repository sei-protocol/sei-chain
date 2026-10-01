package kvrepair

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/kvrepair"
)

const testChainID = "kvrepair-test"

func testKeys() map[string]*sdk.KVStoreKey {
	return sdk.NewKVStoreKeys("evm", "bank")
}

func repairFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys["repairs/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func hexPtr(b []byte) *kvrepair.HexBytes {
	h := kvrepair.HexBytes(b)
	return &h
}

func TestEmbeddedRepairsLoad(t *testing.T) {
	_, err := Load(embeddedRepairs, testKeys())
	require.NoError(t, err)
}

func TestLoadReadsJSONFilesOnly(t *testing.T) {
	repairs, err := Load(repairFS(map[string]string{
		"a.json":    `{"name":"a","chain_id":"c","height":10,"read_height":9,"entries":[{"store":"evm","key":"01","new":"aa","old":"bb"}]}`,
		"README.md": "ignored",
	}), testKeys())
	require.NoError(t, err)
	require.Len(t, repairs, 1)
	require.Equal(t, "a", repairs[0].Name)
}

func TestLoadRejectsInvalidRepairs(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		err   string
	}{
		"parse error names the file": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","ne":"02"}]}`},
			err:   "repairs/a.json: json: unknown field",
		},
		"store without a key": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"nope","key":"01","new":"02"}]}`},
			err:   `repairs/a.json: entry 0: unknown store "nope"`,
		},
		"duplicate name": {
			files: map[string]string{
				"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
				"b.json": `{"name":"a","chain_id":"c","height":3,"read_height":2,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
			},
			err: "already used",
		},
		"same key at same height in two files": {
			files: map[string]string{
				"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","new":"02"}]}`,
				"b.json": `{"name":"b","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","new":"03"}]}`,
			},
			err: "also repaired by",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(repairFS(tc.files), testKeys())
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func newTestContext(keys map[string]*sdk.KVStoreKey, store string) sdk.Context {
	return testutil.DefaultContext(keys[store], sdk.NewTransientStoreKey("transient_test"))
}

func newHandler(keys map[string]*sdk.KVStoreKey, entries ...kvrepair.Entry) Handler {
	return NewHandler(kvrepair.Repair{Name: "r", ChainID: testChainID, Height: 5, Entries: entries}, keys).(Handler)
}

func TestExecuteWritesAndDeletes(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0xde, 0xad})
	store.Set([]byte{2}, []byte{0xde, 0xad})

	h := newHandler(keys,
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{1}, New: hexPtr([]byte{0x01}), Old: hexPtr([]byte{0xde, 0xad})},
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{2}, Old: hexPtr([]byte{0xde, 0xad})},
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{3}, New: hexPtr([]byte{0x03}), OldAbsent: true},
	)
	require.NoError(t, h.ExecuteHandler(ctx))

	require.Equal(t, []byte{0x01}, store.Get([]byte{1}))
	require.Nil(t, store.Get([]byte{2}))
	require.Equal(t, []byte{0x03}, store.Get([]byte{3}))
}

func TestExecuteWritesEntriesThatHoldTheNewValue(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x01})

	var trace bytes.Buffer
	ctx.MultiStore().SetTracer(&trace)
	h := newHandler(keys,
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{1}, New: hexPtr([]byte{0x01}), Old: hexPtr([]byte{0xde, 0xad})},
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{2}, Old: hexPtr([]byte{0xde, 0xad})},
	)
	require.NoError(t, h.ExecuteHandler(ctx))

	require.Equal(t, []byte{0x01}, store.Get([]byte{1}))
	require.Nil(t, store.Get([]byte{2}))
	require.Equal(t, 1, strings.Count(trace.String(), `"operation":"write"`))
	require.Equal(t, 1, strings.Count(trace.String(), `"operation":"delete"`))
}

func TestExecuteFailsWhenOldValueDiffers(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x99})
	store.Set([]byte{2}, []byte{0x99})

	for name, e := range map[string]kvrepair.Entry{
		"other value": {Store: "evm", Key: kvrepair.HexBytes{1}, New: hexPtr([]byte{0x01}), Old: hexPtr([]byte{0xde})},
		"present":     {Store: "evm", Key: kvrepair.HexBytes{2}, New: hexPtr([]byte{0x01}), OldAbsent: true},
		"absent":      {Store: "evm", Key: kvrepair.HexBytes{3}, New: hexPtr([]byte{0x01}), Old: hexPtr([]byte{0xde})},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, newHandler(keys, e).ExecuteHandler(ctx), "expected")
		})
	}
	require.Equal(t, []byte{0x99}, store.Get([]byte{1}))
	require.Equal(t, []byte{0x99}, store.Get([]byte{2}))
	require.Nil(t, store.Get([]byte{3}))
}

func TestExecuteSetsAndChecksEmptyValues(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x99})
	store.Set([]byte{2}, []byte{})

	h := newHandler(keys,
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{1}, New: hexPtr([]byte{}), Old: hexPtr([]byte{0x99})},
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{2}, New: hexPtr([]byte{0x02}), Old: hexPtr([]byte{})},
	)
	require.NoError(t, h.ExecuteHandler(ctx))
	require.Equal(t, []byte{}, store.Get([]byte{1}))
	require.Equal(t, []byte{0x02}, store.Get([]byte{2}))

	absent := newHandler(keys,
		kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{3}, New: hexPtr([]byte{0x03}), Old: hexPtr([]byte{})},
	)
	require.ErrorContains(t, absent.ExecuteHandler(ctx), "expected old value")
	require.Nil(t, store.Get([]byte{3}))
}

func TestExecuteWithoutOldValueOverwrites(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x99})

	h := newHandler(keys, kvrepair.Entry{Store: "evm", Key: kvrepair.HexBytes{1}, New: hexPtr([]byte{0x01})})
	require.NoError(t, h.ExecuteHandler(ctx))
	require.Equal(t, []byte{0x01}, store.Get([]byte{1}))
}

func evmKey(prefix byte, length int) kvrepair.HexBytes {
	key := bytes.Repeat([]byte{0x11}, length+1)
	key[0] = prefix
	return key
}

func TestExecuteTreatsAZeroSlotAsHoldingADelete(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	slot := evmKey(0x03, 52)
	store.Set(slot, make([]byte, 32))

	h := newHandler(keys, kvrepair.Entry{Store: "evm", Key: slot, Old: hexPtr(bytes.Repeat([]byte{0xde}, 32))})
	require.NoError(t, h.ExecuteHandler(ctx))
	require.Nil(t, store.Get(slot))
}

func TestExecuteTreatsAZeroNonceAsAbsentOld(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	nonce := evmKey(0x0a, 20)
	store.Set(nonce, make([]byte, 8))

	target := []byte{0, 0, 0, 0, 0, 0, 0, 7}
	h := newHandler(keys, kvrepair.Entry{Store: "evm", Key: nonce, New: hexPtr(target), OldAbsent: true})
	require.NoError(t, h.ExecuteHandler(ctx))
	require.Equal(t, target, store.Get(nonce))
}

func TestExecuteStillFailsOnAnotherNonZeroValue(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys, "evm")
	store := ctx.KVStore(keys["evm"])
	slot := evmKey(0x03, 52)
	store.Set(slot, bytes.Repeat([]byte{0x99}, 32))

	for name, e := range map[string]kvrepair.Entry{
		"absent old":  {Store: "evm", Key: slot, New: hexPtr(bytes.Repeat([]byte{0x01}, 32)), OldAbsent: true},
		"another old": {Store: "evm", Key: slot, Old: hexPtr(bytes.Repeat([]byte{0xde}, 32))},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, newHandler(keys, e).ExecuteHandler(ctx), "expected")
		})
	}
	require.Equal(t, bytes.Repeat([]byte{0x99}, 32), store.Get(slot))
}

func TestExecuteComparesMiscAndNonEVMKeysExactly(t *testing.T) {
	keys := testKeys()
	for name, tc := range map[string]struct {
		store string
		key   kvrepair.HexBytes
	}{
		"evm misc key":  {store: "evm", key: evmKey(0x09, 20)},
		"non-evm store": {store: "bank", key: evmKey(0x03, 52)},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := newTestContext(keys, tc.store)
			ctx.KVStore(keys[tc.store]).Set(tc.key, make([]byte, 32))
			h := newHandler(keys, kvrepair.Entry{Store: tc.store, Key: tc.key, New: hexPtr([]byte{0x01}), OldAbsent: true})
			require.ErrorContains(t, h.ExecuteHandler(ctx), "expected the key to be absent")
		})
	}
}

func TestHandlerIdentity(t *testing.T) {
	h := newHandler(testKeys())
	require.Equal(t, "kvrepair-r", h.GetName())
	require.Equal(t, testChainID, h.GetTargetChainID())
	require.Equal(t, int64(5), h.GetTargetHeight())
}
