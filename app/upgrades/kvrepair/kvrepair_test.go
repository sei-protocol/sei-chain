package kvrepair

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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

func hexPtr(b []byte) *HexBytes {
	h := HexBytes(b)
	return &h
}

func TestEmbeddedRepairsLoad(t *testing.T) {
	_, err := Load(embeddedRepairs, testKeys())
	require.NoError(t, err)
}

func TestLoadParsesRepair(t *testing.T) {
	fsys := repairFS(map[string]string{
		"a.json": `{"name":"a","chain_id":"c","height":10,"read_height":9,"source":"reserve at 9","entries":[
			{"store":"evm","key":"0x0102","value":"aa","expect":"bb"},
			{"store":"evm","key":"0304","value":null,"expect_absent":false}]}`,
		"README.md": "ignored",
	})
	repairs, err := Load(fsys, testKeys())
	require.NoError(t, err)
	require.Len(t, repairs, 1)
	r := repairs[0]
	require.Equal(t, "a", r.Name)
	require.Equal(t, int64(10), r.Height)
	require.Equal(t, "reserve at 9", r.Source)
	require.Equal(t, HexBytes{1, 2}, r.Entries[0].Key)
	require.Equal(t, hexPtr([]byte{0xaa}), r.Entries[0].Value)
	require.Equal(t, hexPtr([]byte{0xbb}), r.Entries[0].Expect)
	require.Nil(t, r.Entries[1].Value)
}

func TestLoadRejectsInvalidRepairs(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		err   string
	}{
		"unknown field": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","valu":"02"}]}`},
			err:   "unknown field",
		},
		"bad hex": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"zz","value":"02"}]}`},
			err:   "invalid hex",
		},
		"unknown store": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"nope","key":"01","value":"02"}]}`},
			err:   `unknown store "nope"`,
		},
		"no entries": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[]}`},
			err:   "no entries",
		},
		"zero height": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":0,"read_height":1,"entries":[{"store":"evm","key":"01","value":"02"}]}`},
			err:   "not positive",
		},
		"empty value": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":""}]}`},
			err:   "value is empty",
		},
		"both expectations": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":"02","expect":"03","expect_absent":true}]}`},
			err:   "both set",
		},
		"missing value": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","expect":"03"}]}`},
			err:   "value is missing",
		},
		"null expect": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":"02","expect":null}]}`},
			err:   "expect is null",
		},
		"no read height": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"entries":[{"store":"evm","key":"01","value":"02"}]}`},
			err:   "read_height 0 is not positive",
		},
		"read height at height": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":2,"entries":[{"store":"evm","key":"01","value":"02"}]}`},
			err:   "not below height",
		},
		"no expectation with a gap": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":5,"read_height":3,"entries":[{"store":"evm","key":"01","value":"02","expect":"03"},{"store":"evm","key":"02","value":"02"}]}`},
			err:   "entry 1: no expectation",
		},
		"duplicate key in one file": {
			files: map[string]string{"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":"02"},{"store":"evm","key":"01","value":"03"}]}`},
			err:   "appears twice",
		},
		"duplicate name": {
			files: map[string]string{
				"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":"02"}]}`,
				"b.json": `{"name":"a","chain_id":"c","height":3,"read_height":2,"entries":[{"store":"evm","key":"01","value":"02"}]}`,
			},
			err: "already used",
		},
		"same key at same height in two files": {
			files: map[string]string{
				"a.json": `{"name":"a","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":"02"}]}`,
				"b.json": `{"name":"b","chain_id":"c","height":2,"read_height":1,"entries":[{"store":"evm","key":"01","value":"03"}]}`,
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

func TestLoadAcceptsGapWhenEveryEntryHasAnExpectation(t *testing.T) {
	repairs, err := Load(repairFS(map[string]string{
		"a.json": `{"name":"a","chain_id":"c","height":100,"read_height":10,"entries":[
			{"store":"evm","key":"01","value":"02","expect":"03"},
			{"store":"evm","key":"02","value":null,"expect_absent":true}]}`,
	}), testKeys())
	require.NoError(t, err)
	require.Len(t, repairs, 1)
	require.Equal(t, int64(10), repairs[0].ReadHeight)
}

func newTestContext(keys map[string]*sdk.KVStoreKey) sdk.Context {
	return testutil.DefaultContext(keys["evm"], sdk.NewTransientStoreKey("transient_test"))
}

func TestExecuteWritesAndDeletes(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys)
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0xde, 0xad})
	store.Set([]byte{2}, []byte{0xde, 0xad})

	h := NewHandler(Repair{Name: "r", ChainID: testChainID, Height: 5, Entries: []Entry{
		{Store: "evm", Key: HexBytes{1}, Value: hexPtr([]byte{0x01}), Expect: hexPtr([]byte{0xde, 0xad})},
		{Store: "evm", Key: HexBytes{2}, Expect: hexPtr([]byte{0xde, 0xad})},
		{Store: "evm", Key: HexBytes{3}, Value: hexPtr([]byte{0x03}), ExpectAbsent: true},
	}}, keys)
	require.NoError(t, h.ExecuteHandler(ctx))

	require.Equal(t, []byte{0x01}, store.Get([]byte{1}))
	require.Nil(t, store.Get([]byte{2}))
	require.Equal(t, []byte{0x03}, store.Get([]byte{3}))
}

func TestExecuteWritesEntriesThatHoldTheTarget(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys)
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x01})

	var trace bytes.Buffer
	ctx.MultiStore().SetTracer(&trace)
	h := NewHandler(Repair{Name: "r", ChainID: testChainID, Height: 5, Entries: []Entry{
		{Store: "evm", Key: HexBytes{1}, Value: hexPtr([]byte{0x01}), Expect: hexPtr([]byte{0xde, 0xad})},
		{Store: "evm", Key: HexBytes{2}, Expect: hexPtr([]byte{0xde, 0xad})},
	}}, keys)
	require.NoError(t, h.ExecuteHandler(ctx))

	require.Equal(t, []byte{0x01}, store.Get([]byte{1}))
	require.Nil(t, store.Get([]byte{2}))
	require.Equal(t, 1, strings.Count(trace.String(), `"operation":"write"`))
	require.Equal(t, 1, strings.Count(trace.String(), `"operation":"delete"`))
}

func TestExecuteFailsOnUnexpectedValue(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys)
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x99})
	store.Set([]byte{2}, []byte{0x99})

	for name, e := range map[string]Entry{
		"other value": {Store: "evm", Key: HexBytes{1}, Value: hexPtr([]byte{0x01}), Expect: hexPtr([]byte{0xde})},
		"present":     {Store: "evm", Key: HexBytes{2}, Value: hexPtr([]byte{0x01}), ExpectAbsent: true},
		"absent":      {Store: "evm", Key: HexBytes{3}, Value: hexPtr([]byte{0x01}), Expect: hexPtr([]byte{0xde})},
	} {
		t.Run(name, func(t *testing.T) {
			h := NewHandler(Repair{Name: "r", ChainID: testChainID, Height: 5, Entries: []Entry{e}}, keys)
			require.ErrorContains(t, h.ExecuteHandler(ctx), "expected")
		})
	}
	require.Equal(t, []byte{0x99}, store.Get([]byte{1}))
	require.Equal(t, []byte{0x99}, store.Get([]byte{2}))
	require.Nil(t, store.Get([]byte{3}))
}

func TestExecuteWithoutExpectationOverwrites(t *testing.T) {
	keys := testKeys()
	ctx := newTestContext(keys)
	store := ctx.KVStore(keys["evm"])
	store.Set([]byte{1}, []byte{0x99})

	h := NewHandler(Repair{Name: "r", ChainID: testChainID, Height: 5, Entries: []Entry{
		{Store: "evm", Key: HexBytes{1}, Value: hexPtr([]byte{0x01})},
	}}, keys)
	require.NoError(t, h.ExecuteHandler(ctx))
	require.Equal(t, []byte{0x01}, store.Get([]byte{1}))
}

func TestHandlerIdentity(t *testing.T) {
	h := NewHandler(Repair{Name: "r", ChainID: testChainID, Height: 5}, testKeys())
	require.Equal(t, "kvrepair-r", h.GetName())
	require.Equal(t, testChainID, h.GetTargetChainID())
	require.Equal(t, int64(5), h.GetTargetHeight())
}
