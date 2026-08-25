package app

import (
	"testing"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// TestKVStoreKeyNamesMatchMemIAVLStoreKeys ensures the slice consumed by
// sdk.NewKVStoreKeys in app.New stays in lock-step with the dependency-free
// canonical list in sei-db/common/keys. If a store is added to or removed
// from kvStoreKeyNames, MemIAVLStoreKeys must be updated to match (and
// vice versa).
func TestKVStoreKeyNamesMatchMemIAVLStoreKeys(t *testing.T) {
	require.Equal(t, keys.MemIAVLStoreKeys, kvStoreKeyNames,
		"app.kvStoreKeyNames (passed to sdk.NewKVStoreKeys in app.New) "+
			"is out of sync with sei-db/common/keys.MemIAVLStoreKeys; "+
			"update both lists together")
}

func TestFeegrantStoreRemainsMounted(t *testing.T) {
	require.Contains(t, kvStoreKeyNames, keys.FeegrantStoreKey)

	testApp := Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{})
	store := ctx.KVStore(testApp.GetKey(keys.FeegrantStoreKey))
	store.Set([]byte("allowance"), []byte("retained"))
	require.Equal(t, []byte("retained"), store.Get([]byte("allowance")))
}

func TestTransferStoreRemainsMounted(t *testing.T) {
	require.Contains(t, kvStoreKeyNames, keys.IBCTransferStoreKey)

	testApp := Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{})
	store := ctx.KVStore(testApp.GetKey(keys.IBCTransferStoreKey))
	store.Set([]byte("denom-trace"), []byte("retained"))
	require.Equal(t, []byte("retained"), store.Get([]byte("denom-trace")))
}

func TestRetiredTransferModuleAccountRemainsMaterialized(t *testing.T) {
	testApp := Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{})

	account := testApp.AccountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(transferModuleName))
	moduleAccount, ok := account.(authtypes.ModuleAccountI)
	require.True(t, ok)
	require.Equal(t, transferModuleName, moduleAccount.GetName())
	require.ElementsMatch(t, []string{authtypes.Minter, authtypes.Burner}, moduleAccount.GetPermissions())
}

func TestCapabilityStoreRemainsMounted(t *testing.T) {
	require.Contains(t, kvStoreKeyNames, keys.CapabilityStoreKey)

	testApp := Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{})
	store := ctx.KVStore(testApp.GetKey(keys.CapabilityStoreKey))
	store.Set([]byte("owner"), []byte("retained"))
	require.Equal(t, []byte("retained"), store.Get([]byte("owner")))
}
