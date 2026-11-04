package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

func TestLocalMetaDecoder(t *testing.T) {
	t.Run("FullRecord", func(t *testing.T) {
		root := lthash.ComputeLtHash(nil, []mutation.Mutation{{Key: []byte("evm/k"), NewValue: []byte("v")}})
		stats := map[string]lthash.ModuleStats{"evm": {KeyCount: 1, Bytes: 6}}
		pairs, err := encodeLocalMeta(42, root, map[string]*lthash.LtHash{"evm": root}, stats)
		require.NoError(t, err)

		db := setupTestDB(t)
		for _, pair := range pairs {
			require.NoError(t, db.Set(pair.Key, pair.Value, types.WriteOptions{}))
		}
		meta, err := loadLocalMeta(db)
		require.NoError(t, err)
		require.Equal(t, int64(42), meta.CommittedVersion)
		require.True(t, root.Equal(meta.LtHash))
		require.True(t, root.Equal(meta.ModuleLtHashes["evm"]))
		require.Equal(t, stats, meta.ModuleStats)
	})

	t.Run("Empty", func(t *testing.T) {
		var decoder LocalMetaDecoder
		meta, err := decoder.Finish()
		require.NoError(t, err)
		require.Equal(t, &LocalMeta{CommittedVersion: 0}, meta)
	})

	t.Run("MissingVersionWithOtherMetadata", func(t *testing.T) {
		var decoder LocalMetaDecoder
		decoder.Add(ktype.MetaLtHashKey, lthash.New().Marshal())
		_, err := decoder.Finish()
		require.ErrorContains(t, err, "is absent but _meta/hash is present")
	})

	t.Run("UnknownMetaKeysAreIgnored", func(t *testing.T) {
		var decoder LocalMetaDecoder
		decoder.Add([]byte("_meta/unknown"), []byte("anything"))
		decoder.Add(ktype.MetaVersionKey, versionToBytes(7))
		meta, err := decoder.Finish()
		require.NoError(t, err)
		require.Equal(t, int64(7), meta.CommittedVersion)
		require.Nil(t, meta.LtHash)
		require.Empty(t, meta.ModuleLtHashes)
		require.Empty(t, meta.ModuleStats)
	})
}
