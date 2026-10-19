package rootmulti

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/storev2/query"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

// commitNonceOnlyAccounts commits n EVM accounts that hold a nonce and nothing else, the state shape
// that makes flatkv's code-hash and balance lanes read every account row and yield none.
func commitNonceOnlyAccounts(t *testing.T, store *Store, evmKey *types.KVStoreKey, n int) commitRecord {
	t.Helper()
	cms := store.CacheMultiStore()
	kvStore := cms.GetKVStore(evmKey)
	for i := 0; i < n; i++ {
		var addr [20]byte
		binary.BigEndian.PutUint32(addr[16:], uint32(i)) //nolint:gosec // small test index
		kvStore.Set(keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), makeNonce(1))
	}
	cms.Write()
	return finalizeBlock(t, store)
}

// subspaceQuery runs one /evm/subspace query for the logical prefix of kind.
func subspaceQuery(t *testing.T, store *Store, kind keys.EVMKeyKind, height int64) abci.ResponseQuery {
	t.Helper()
	prefix, ok := keys.EVMKeyPrefixByte(kind)
	require.True(t, ok)
	return store.Query(context.Background(), abci.RequestQuery{
		Path:   "/evm/subspace",
		Data:   []byte{prefix},
		Height: height,
	})
}

// TestFlatKVSubspaceScanBudget pins that a /subspace query over flatkv is bounded by the rows it reads,
// not only by the rows it returns. A code-hash or balance scan over nonce-only accounts returns nothing,
// and must still be refused once it has read more account rows than the scan budget, at the latest and
// at a historical height. With SS enabled the query is served from logical keys, where there
// are no hidden rows, and succeeds.
func TestFlatKVSubspaceScanBudget(t *testing.T) {
	const accounts = 40

	for _, tc := range []struct {
		name      string
		ssEnabled bool
	}{
		{name: "ss disabled", ssEnabled: false},
		{name: "ss enabled", ssEnabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scCfg := flatKVOnlyConfig()
			ssCfg := seidbconfig.StateStoreConfig{}
			if tc.ssEnabled {
				ssCfg = seidbconfig.DefaultStateStoreConfig()
				ssCfg.Enable = true
				ssCfg.AsyncWriteBuffer = 0
			}
			store, storeKeys := newTestRootMultiWithSS(t, t.TempDir(), scCfg, ssCfg)
			defer func() { require.NoError(t, store.Close()) }()
			store.subspaceLimits.MaxScannedRows = accounts / 2

			c1 := commitNonceOnlyAccounts(t, store, storeKeys["evm"], accounts)
			c2 := finalizeBlock(t, store)
			if tc.ssEnabled {
				waitUntilSSVersion(t, store, c2.version)
			}

			for _, height := range []int64{c2.version, c1.version} {
				for _, kind := range []keys.EVMKeyKind{keys.EVMKeyCodeHash, keys.EVMKeyBalance} {
					resp := subspaceQuery(t, store, kind, height)
					if tc.ssEnabled {
						require.EqualValues(t, 0, resp.Code, "height %d kind %v: %s", height, kind, resp.Log)
						var pairs kv.Pairs
						require.NoError(t, pairs.Unmarshal(resp.Value))
						require.Empty(t, pairs.Pairs)
						continue
					}
					require.True(t, query.IsCapExceededResponse(resp),
						"height %d kind %v: want a cap rejection, got code %d: %s", height, kind, resp.Code, resp.Log)
				}
			}
			require.Empty(t, store.subspaceQuerySem, "a refused scan must hand back its permit")
		})
	}
}

// TestFlatKVSubspaceScanBudgetCoversTheScan pins that a budget of one row per account is enough for a
// code-hash scan over those accounts, so the bound refuses only scans that read more than it allows.
func TestFlatKVSubspaceScanBudgetCoversTheScan(t *testing.T) {
	const accounts = 40
	store, storeKeys := newTestRootMulti(t, t.TempDir(), flatKVOnlyConfig())
	defer func() { require.NoError(t, store.Close()) }()
	store.subspaceLimits.MaxScannedRows = accounts

	c1 := commitNonceOnlyAccounts(t, store, storeKeys["evm"], accounts)

	resp := subspaceQuery(t, store, keys.EVMKeyCodeHash, c1.version)
	require.EqualValues(t, 0, resp.Code, resp.Log)
	var pairs kv.Pairs
	require.NoError(t, pairs.Unmarshal(resp.Value))
	require.Empty(t, pairs.Pairs)
}
