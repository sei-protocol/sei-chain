package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetChainID(t *testing.T) {
	k, _, _ := MockEVMKeeper()
	require.Equal(t, int64(1), k.ChainID().Int64())
}
