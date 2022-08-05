package dex_test

import (
	"testing"

	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/stretchr/testify/require"
)

func TestDepositFilterByAccount(t *testing.T) {
	deposits := dex.NewDepositInfo()
	deposit := dex.DepositInfoEntry{
		Creator: "abc",
	}
	deposits.Add(&deposit)
	deposits.FilterByAccount("abc")
	require.Equal(t, 0, len(deposits.Get()))
}
