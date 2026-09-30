package types

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func testBlockHash(b byte) []byte {
	return bytes.Repeat([]byte{b}, tmhash.Size)
}

func TestEmbeddedRewindsLoad(t *testing.T) {
	_, err := LoadRewinds(embeddedRewinds)
	require.NoError(t, err)
}

func TestRewindLookups(t *testing.T) {
	t.Cleanup(ReplaceRewinds([]Rewind{{
		ChainID:    "chain-a",
		SafeHeight: 10,
		Discarded:  []DiscardedBlock{{Height: 11, Hash: testBlockHash(1)}, {Height: 12, Hash: testBlockHash(2)}},
	}}))

	require.True(t, IsDiscardedBlock("chain-a", 11, testBlockHash(1)))
	require.True(t, IsDiscardedBlock("chain-a", 12, testBlockHash(2)))
	require.False(t, IsDiscardedBlock("chain-a", 11, testBlockHash(2)), "replacement block")
	require.False(t, IsDiscardedBlock("chain-a", 10, testBlockHash(1)), "safe height")
	require.False(t, IsDiscardedBlock("chain-b", 11, testBlockHash(1)), "other chain")

	for height, want := range map[int64]bool{9: false, 10: false, 11: true, 12: true, 13: false} {
		require.Equal(t, want, InRewoundWindow("chain-a", height), "height %d", height)
	}
	require.False(t, InRewoundWindow("chain-b", 11))
}

func TestLoadRewinds(t *testing.T) {
	file := fmt.Sprintf(`{"chain_id":"chain-a","source":"test","safe_height":10,
		"discarded":[{"height":11,"hash":"%X"},{"height":12,"hash":"%X"}]}`, testBlockHash(1), testBlockHash(2))
	rs, err := LoadRewinds(fstest.MapFS{
		"rewinds/a.json":   {Data: []byte(file + "\n")},
		"rewinds/.gitkeep": {Data: nil},
	})
	require.NoError(t, err)
	require.Equal(t, []Rewind{{
		ChainID:    "chain-a",
		Source:     "test",
		SafeHeight: 10,
		Discarded:  []DiscardedBlock{{Height: 11, Hash: testBlockHash(1)}, {Height: 12, Hash: testBlockHash(2)}},
	}}, rs)
}

func TestLoadRewindsRejects(t *testing.T) {
	hash := strings.ToUpper(fmt.Sprintf("%x", testBlockHash(1)))
	rewind := func(chainID string, safe int64, heights ...int64) string {
		discarded := make([]string, 0, len(heights))
		for _, h := range heights {
			discarded = append(discarded, fmt.Sprintf(`{"height":%d,"hash":"%s"}`, h, hash))
		}
		return fmt.Sprintf(`{"chain_id":%q,"safe_height":%d,"discarded":[%s]}`, chainID, safe, strings.Join(discarded, ","))
	}
	testcases := map[string][]string{
		"unknown field":         {`{"chain_id":"c","safe_height":10,"discarded":[],"extra":1}`},
		"no chain id":           {rewind("", 10, 11)},
		"zero safe height":      {rewind("c", 0, 1)},
		"no discarded blocks":   {rewind("c", 10)},
		"gap after safe height": {rewind("c", 10, 12)},
		"gap between blocks":    {rewind("c", 10, 11, 13)},
		"short hash":            {`{"chain_id":"c","safe_height":10,"discarded":[{"height":11,"hash":"AB"}]}`},
		"overlap across files":  {rewind("c", 10, 11, 12), rewind("c", 11, 12)},
		"same window twice":     {rewind("c", 10, 11), rewind("c", 10, 11)},
		"hash is not hex":       {`{"chain_id":"c","safe_height":10,"discarded":[{"height":11,"hash":"zz"}]}`},
		"second object":         {rewind("c", 10, 11) + "\n" + rewind("c", 20, 21)},
		"trailing garbage":      {rewind("c", 10, 11) + " xyz"},
		"field twice in a block": {fmt.Sprintf(`{"chain_id":"c","safe_height":10,"discarded":[{"height":11,"hash":"%s","hash":"%X"}]}`,
			hash, testBlockHash(2))},
		"field twice at the top": {fmt.Sprintf(`{"chain_id":"c","safe_height":10,"safe_height":10,"discarded":[{"height":11,"hash":"%s"}]}`, hash)},
		"field in another case":  {fmt.Sprintf(`{"chain_id":"c","Safe_Height":10,"discarded":[{"height":11,"hash":"%s"}]}`, hash)},
	}
	for name, files := range testcases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for i, data := range files {
				fsys[fmt.Sprintf("rewinds/%d.json", i)] = &fstest.MapFile{Data: []byte(data)}
			}
			_, err := LoadRewinds(fsys)
			require.Error(t, err)
		})
	}

	_, err := LoadRewinds(fstest.MapFS{
		"rewinds/0.json": {Data: []byte(rewind("c", 10, 11))},
		"rewinds/1.json": {Data: []byte(rewind("c", 11, 12))},
		"rewinds/2.json": {Data: []byte(rewind("other", 10, 11))},
	})
	require.NoError(t, err, "adjacent windows and other chains are allowed")
}
