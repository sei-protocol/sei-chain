package segment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/test"
	"github.com/stretchr/testify/require"
)

func TestAddress(t *testing.T) {
	t.Parallel()
	rand := test.NewTestRandom()

	index := rand.Uint32()
	offset := rand.Uint32()
	shard := uint8(rand.Uint32())
	valueSize := rand.Uint32()
	address := types.NewAddress(index, offset, shard, valueSize)

	require.Equal(t, index, address.Index())
	require.Equal(t, offset, address.Offset())
	require.Equal(t, shard, address.Shard())
	require.Equal(t, valueSize, address.ValueSize())

	serialized := address.Serialize()
	deserialized, err := types.DeserializeAddress(serialized)
	require.NoError(t, err)
	require.Equal(t, address, deserialized)
}
