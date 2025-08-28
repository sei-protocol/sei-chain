package types_test

import (
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAddERCNativePointerProposalV2(t *testing.T) {
	p := types.AddERCNativePointerProposalV2{
		Title:       "title",
		Description: "desc",
		Token:       "test",
		Name:        "TEST",
		Symbol:      "Test",
		Decimals:    6,
	}
	require.Equal(t, "title", p.GetTitle())
	require.Equal(t, "desc", p.GetDescription())
	require.Equal(t, "evm", p.ProposalRoute())
	require.Equal(t, "AddERCNativePointerV2", p.ProposalType())
	p.Decimals = math.MaxUint32
	require.NotNil(t, p.ValidateBasic())
	p.Decimals = 6
	require.Nil(t, p.ValidateBasic())
	require.NotEmpty(t, p.String())
}
