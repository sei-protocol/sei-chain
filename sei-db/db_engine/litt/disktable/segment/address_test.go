//go:build littdb_wip

package segment

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

func TestAddress(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	index := rand.Uint32()
	offset := rand.Uint32()
	address := types.NewAddress(index, offset)

	require.Equal(t, index, address.Index())
	require.Equal(t, offset, address.Offset())
}
