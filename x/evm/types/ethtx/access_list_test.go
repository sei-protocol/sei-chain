package ethtx

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestAccessList(t *testing.T) {
	require.Nil(t, NewAccessList(nil))
	ethAccessList := mockAccessList()
	require.Equal(t, ethAccessList, *NewAccessList(&ethAccessList).ToEthAccessList())
}

func mockAccessList() ethtypes.AccessList {
	return ethtypes.AccessList{
		ethtypes.AccessTuple{
			Address:     common.Address{'a'},
			StorageKeys: []common.Hash{{'b'}},
		},
	}
}
