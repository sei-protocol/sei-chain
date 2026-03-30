package wrappers

import (
	"bytes"
	"testing"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

func TestStateStoreWrapperApplyChangesetsAsyncPreservesHistoricalState(t *testing.T) {
	dataDir := t.TempDir()

	store, err := openSSComposite(dataDir)
	require.NoError(t, err)

	wrapper := NewStateStoreWrapper(store)

	keyV1AndV2 := commonevm.BuildMemIAVLEVMKey(commonevm.EVMKeyNonce, bytes.Repeat([]byte{0x11}, 20))
	keyV2Only := commonevm.BuildMemIAVLEVMKey(commonevm.EVMKeyCodeHash, bytes.Repeat([]byte{0x22}, 20))

	require.NoError(t, wrapper.ApplyChangeSets(namedChangeSet(keyV1AndV2, []byte("value-v1"))))

	version, err := wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)

	require.NoError(t, wrapper.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: EVMStoreName,
			Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
				{Key: keyV1AndV2, Value: []byte("value-v2")},
				{Key: keyV2Only, Value: []byte("value-v2-only")},
			}},
		},
	}))

	version, err = wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(2), version)

	require.NoError(t, wrapper.Close())

	reopened, err := openSSComposite(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, reopened.Close())
	})

	historical, err := reopened.Get(EVMStoreName, 1, keyV1AndV2)
	require.NoError(t, err)
	require.Equal(t, []byte("value-v1"), historical)

	missingAtV1, err := reopened.Get(EVMStoreName, 1, keyV2Only)
	require.NoError(t, err)
	require.Nil(t, missingAtV1)

	latest, err := reopened.Get(EVMStoreName, 2, keyV1AndV2)
	require.NoError(t, err)
	require.Equal(t, []byte("value-v2"), latest)

	latestOnly, err := reopened.Get(EVMStoreName, 2, keyV2Only)
	require.NoError(t, err)
	require.Equal(t, []byte("value-v2-only"), latestOnly)
}

func namedChangeSet(key, value []byte) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{
		{
			Name: EVMStoreName,
			Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
				{Key: key, Value: value},
			}},
		},
	}
}
