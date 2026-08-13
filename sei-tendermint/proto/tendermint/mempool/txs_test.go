package mempool

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	txs := Txs{Txs: [][]byte{}}
	require.Contains(t, txs.Validate().Error(), "empty txs received from peer")
	txs.Txs = [][]byte{{0}, {0}}
	require.Contains(t, txs.Validate().Error(), "right now we only allow 1 tx per envelope")
	txs.Txs = [][]byte{{0}}
	require.Nil(t, txs.Validate())
}
