package types

import (
	"testing"
	"testing/fstest"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEmbeddedAppHashOverridesLoad(t *testing.T) {
	_, err := LoadAppHashOverrides(embeddedAppHashOverrides)
	require.NoError(t, err)
}

func TestAppHashMatches(t *testing.T) {
	recorded := []byte{0xc1}
	replacement := []byte{0xa1}
	t.Cleanup(ReplaceAppHashOverrides([]AppHashOverride{
		{ChainID: "chain-a", Height: 10, Recorded: recorded, Replacement: replacement},
	}))

	testcases := map[string]struct {
		chainID            string
		height             int64
		recorded, computed []byte
		want               bool
	}{
		"equal hashes":           {"chain-b", 5, recorded, recorded, true},
		"override":               {"chain-a", 10, recorded, replacement, true},
		"different chain":        {"chain-b", 10, recorded, replacement, false},
		"different height":       {"chain-a", 11, recorded, replacement, false},
		"different recorded":     {"chain-a", 10, []byte{0xc2}, replacement, false},
		"different computed":     {"chain-a", 10, recorded, []byte{0xa2}, false},
		"override used reversed": {"chain-a", 10, replacement, recorded, false},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, AppHashMatches(tc.chainID, tc.height, tc.recorded, tc.computed))
		})
	}
}

func TestReplaceAppHashOverridesRestores(t *testing.T) {
	o := AppHashOverride{ChainID: "chain-a", Height: 10, Recorded: []byte{1}, Replacement: []byte{2}}
	restore := ReplaceAppHashOverrides([]AppHashOverride{o})
	require.True(t, AppHashMatches("chain-a", 10, []byte{1}, []byte{2}))
	restore()
	require.False(t, AppHashMatches("chain-a", 10, []byte{1}, []byte{2}))
}

func TestLoadAppHashOverrides(t *testing.T) {
	fsys := fstest.MapFS{
		"apphash_overrides/a.json": {Data: []byte(`{"chain_id":"chain-a","source":"test",
			"overrides":[{"height":10,"recorded":"C1","replacement":"A1"},{"height":11,"recorded":"C2","replacement":"A2"}]}`)},
		"apphash_overrides/b.json":   {Data: []byte(`{"chain_id":"chain-b","overrides":[{"height":10,"recorded":"C1","replacement":"A1"}]}`)},
		"apphash_overrides/.gitkeep": {Data: nil},
	}
	overrides, err := LoadAppHashOverrides(fsys)
	require.NoError(t, err)
	require.Equal(t, []AppHashOverride{
		{ChainID: "chain-a", Height: 10, Recorded: []byte{0xc1}, Replacement: []byte{0xa1}},
		{ChainID: "chain-a", Height: 11, Recorded: []byte{0xc2}, Replacement: []byte{0xa2}},
		{ChainID: "chain-b", Height: 10, Recorded: []byte{0xc1}, Replacement: []byte{0xa1}},
	}, overrides)
}

func TestLoadAppHashOverridesRejects(t *testing.T) {
	row := `{"height":10,"recorded":"C1","replacement":"A1"}`
	testcases := map[string][]string{
		"unknown field":           {`{"chain_id":"c","overrides":[` + row + `],"extra":1}`},
		"no chain id":             {`{"overrides":[` + row + `]}`},
		"no overrides":            {`{"chain_id":"c","overrides":[]}`},
		"zero height":             {`{"chain_id":"c","overrides":[{"height":0,"recorded":"C1","replacement":"A1"}]}`},
		"empty recorded":          {`{"chain_id":"c","overrides":[{"height":10,"recorded":"","replacement":"A1"}]}`},
		"recorded is replacement": {`{"chain_id":"c","overrides":[{"height":10,"recorded":"A1","replacement":"A1"}]}`},
		"not hex":                 {`{"chain_id":"c","overrides":[{"height":10,"recorded":"zz","replacement":"A1"}]}`},
		"duplicate in a file":     {`{"chain_id":"c","overrides":[` + row + `,` + row + `]}`},
		"duplicate across files":  {`{"chain_id":"c","overrides":[` + row + `]}`, `{"chain_id":"c","overrides":[` + row + `]}`},
	}
	for name, files := range testcases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for i, data := range files {
				fsys["apphash_overrides/"+string(rune('a'+i))+".json"] = &fstest.MapFile{Data: []byte(data)}
			}
			_, err := LoadAppHashOverrides(fsys)
			require.Error(t, err)
		})
	}
}
