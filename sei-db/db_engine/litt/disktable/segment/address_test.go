package segment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/common/test/random"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/stretchr/testify/require"
)

func TestAddress(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()

	index := rand.Uint32()
	offset := rand.Uint32()
	address := types.NewAddress(index, offset)

	require.Equal(t, index, address.Index())
	require.Equal(t, offset, address.Offset())
}
