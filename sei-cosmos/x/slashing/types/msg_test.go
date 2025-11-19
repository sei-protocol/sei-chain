package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestMsgUnjailGetSignBytes(t *testing.T) {
	addr := seitypes.AccAddress("abcd")
	msg := NewMsgUnjail(seitypes.ValAddress(addr))
	bytes := msg.GetSignBytes()
	require.Equal(
		t,
		`{"type":"cosmos-sdk/MsgUnjail","value":{"address":"seivaloper1v93xxeqlus7yn"}}`,
		string(bytes),
	)
}
