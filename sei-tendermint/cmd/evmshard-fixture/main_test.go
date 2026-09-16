package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// TestGolden pins Committee.EvmShard to the mappings the checked-in sei-load
// fixtures were generated from. If this fails, EvmShard changed: regenerate
// the fixtures in sei-load (see the package doc) and update the pairs below.
func TestGolden(t *testing.T) {
	for _, tc := range []struct {
		weights string
		want    map[common.Address]int
	}{
		{
			weights: "1,1,1,1",
			want: map[common.Address]int{
				common.HexToAddress("0xb3972348930b8ce083b85793ecf520bcee2368af"): 2,
				common.HexToAddress("0x68019dfce1f88e59b32deaad9601f495df148a66"): 0,
				common.HexToAddress("0xafcbb36b936ce04dfd2ee3dda528db75e0ea7a68"): 1,
				common.HexToAddress("0x1b44a9edd0db5cb3807f62c17967716f3e7cefbb"): 0,
				common.HexToAddress("0xddb8c6f97215f143bd7bc4a80cbbe6f34befbcfc"): 3,
				common.HexToAddress("0x58b9ccec47ab79f5b7f9b22629177e9f5601bd93"): 1,
			},
		},
		{
			weights: "7,1,1,1",
			want: map[common.Address]int{
				common.HexToAddress("0xb3972348930b8ce083b85793ecf520bcee2368af"): 0,
				common.HexToAddress("0x68019dfce1f88e59b32deaad9601f495df148a66"): 0,
				common.HexToAddress("0xafcbb36b936ce04dfd2ee3dda528db75e0ea7a68"): 0,
				common.HexToAddress("0x1b44a9edd0db5cb3807f62c17967716f3e7cefbb"): 0,
				common.HexToAddress("0xddb8c6f97215f143bd7bc4a80cbbe6f34befbcfc"): 0,
				common.HexToAddress("0x58b9ccec47ab79f5b7f9b22629177e9f5601bd93"): 1,
			},
		},
	} {
		t.Run(tc.weights, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, run(tc.weights, 1000, "evmshard-fixture", &buf))
			var f fixture
			require.NoError(t, json.Unmarshal(buf.Bytes(), &f))
			require.Len(t, f.Validators, 4)
			require.Len(t, f.Owners, 1000)
			got := make(map[common.Address]int, len(f.Owners))
			for _, o := range f.Owners {
				got[o.Address] = o.Validator
			}
			for addr, want := range tc.want {
				owner, ok := got[addr]
				require.True(t, ok, "address %s missing from fixture", addr)
				require.Equal(t, want, owner, "owner of %s", addr)
			}
		})
	}
}
