package types_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestIsAssociate(t *testing.T) {
	msg, err := types.NewMsgEVMTransaction(&ethtx.AssociateTx{})
	require.Nil(t, err)
	require.True(t, msg.IsAssociateTx())
}

func TestAttackerUnableToSetDerived(t *testing.T) {
	msg := types.MsgEVMTransaction{Derived: &derived.Derived{SenderEVMAddr: common.BytesToAddress([]byte("abc"))}}
	bz, err := msg.Marshal()
	require.Nil(t, err)
	decoded := types.MsgEVMTransaction{}
	err = decoded.Unmarshal(bz)
	require.Nil(t, err)
	require.Equal(t, common.Address{}, decoded.Derived.SenderEVMAddr)
}
