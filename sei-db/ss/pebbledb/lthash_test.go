package pebbledb

import (
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLtHashCommitment(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "pebbledb-lthash-test")
	require.NoError(t, err)
	defer os.RemoveAll(dataDir)

	cfg := config.DefaultStateStoreConfig()
	db, err := New(dataDir, cfg)
	require.NoError(t, err)
	defer db.Close()

	// Initial hash should be identity
	initialHash := db.GetCommitHash()
	assert.Equal(t, int64(0), initialHash.Version)
	assert.True(t, db.GetLtHash().IsIdentity())

	// Commit some data
	version := int64(1)
	changeSets := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: makeAddr(1), Value: []byte{0x01}, Delete: false},
				},
			},
		},
	}

	stateHash, metaKVs := db.ApplyCommitHash(version, changeSets, nil)
	assert.Equal(t, version, stateHash.Version)
	assert.False(t, db.GetLtHash().IsIdentity())
	assert.NotEmpty(t, stateHash.Hash)
	assert.NotEmpty(t, metaKVs)

	// Persist the changes
	// We need to append metaKVs manually if we are testing pebbledb directly
	for _, kv := range metaKVs {
		changeSets[0].Changeset.Pairs = append(changeSets[0].Changeset.Pairs, &iavl.KVPair{
			Key:    kv.Key,
			Value:  kv.Value,
			Delete: kv.Deleted,
		})
	}
	err = db.ApplyChangesetSync(version, changeSets)
	require.NoError(t, err)

	// Check if LtHash is persisted
	err = db.Close()
	require.NoError(t, err)

	db2, err := New(dataDir, cfg)
	require.NoError(t, err)
	defer db2.Close()

	assert.Equal(t, stateHash.Hash, db2.GetCommitHash().Hash)
	assert.Equal(t, version, db2.GetCommitHash().Version)
	assert.False(t, db2.GetLtHash().IsIdentity())

	// Test incremental update (MixOut)
	version2 := int64(2)
	changeSets2 := []*proto.NamedChangeSet{
		{
			Name: "bank",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: makeAddr(1), Value: nil, Delete: true}, // Delete key 1
				},
			},
		},
	}

	stateHash2, _ := db2.ApplyCommitHash(version2, changeSets2, nil)
	assert.NotEqual(t, stateHash.Hash, stateHash2.Hash)
	assert.True(t, db2.GetLtHash().IsIdentity()) // Should be identity again after deleting the only key
}

func makeAddr(i int) []byte {
	addr := make([]byte, 20)
	addr[19] = byte(i)
	return addr
}
