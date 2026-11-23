package app

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
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
