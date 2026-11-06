package rootmulti

import (
	"testing"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

// TestQuery_SubspaceRequiresSS pins that a node with SS disabled refuses every /subspace query, on
// memiavl and on flatkv, at the latest and at a historical height, before it takes a permit, and
// still serves /key.
func TestQuery_SubspaceRequiresSS(t *testing.T) {
	codeHashPrefix, ok := keys.EVMKeyPrefixByte(keys.EVMKeyCodeHash)
	require.True(t, ok)

	for _, tc := range []struct {
		name string
		cfg  seidbconfig.StateCommitConfig
	}{
		{name: "memiavl", cfg: memiavlOnlyConfig()},
		{name: "flatkv", cfg: flatKVOnlyConfig()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, storeKeys := newTestRootMulti(t, t.TempDir(), tc.cfg)
			defer func() { require.NoError(t, store.Close()) }()

			data := newEVMTestData(1)
			c1 := simulateBlock(t, store, storeKeys, 1, data)
			c2 := simulateBlock(t, store, storeKeys, 2, data)

			for _, height := range []int64{c2.version, c1.version} {
				for _, req := range []abci.RequestQuery{
					{Path: "/evm/subspace", Data: []byte{codeHashPrefix}, Height: height},
					{Path: "/bank/subspace", Data: []byte("s"), Height: height},
				} {
					resp := store.Query(t.Context(), req)
					require.Equal(t, sdkerrors.ErrNotSupported.Codespace(), resp.Codespace, "%s at %d: %s", req.Path, height, resp.Log)
					require.Equal(t, sdkerrors.ErrNotSupported.ABCICode(), resp.Code, "%s at %d: %s", req.Path, height, resp.Log)
				}

				resp := store.Query(t.Context(), abci.RequestQuery{Path: "/bank/key", Data: []byte("supply"), Height: height})
				require.EqualValues(t, 0, resp.Code, "key at %d: %s", height, resp.Log)
				require.NotEmpty(t, resp.Value)
			}
			require.Empty(t, store.subspaceQuerySem)

			for len(store.subspaceQuerySem) < cap(store.subspaceQuerySem) {
				store.subspaceQuerySem <- struct{}{}
			}
			resp := store.Query(t.Context(), abci.RequestQuery{Path: "/bank/subspace", Data: []byte("s")})
			require.Equal(t, sdkerrors.ErrNotSupported.ABCICode(), resp.Code, "a full semaphore must not hide the refusal: %s", resp.Log)
		})
	}
}
