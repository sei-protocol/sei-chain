package simulation_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	epochsimulation "github.com/sei-protocol/sei-chain/x/epoch/simulation"

	"github.com/stretchr/testify/require"
)

func TestFindAccount(t *testing.T) {
	// Setup
	var accs []simtypes.Account
	accs = append(accs, simtypes.Account{
		Address: sdk.AccAddress([]byte("sei1qzdrwc3806zfdl98608nqnsvhg8hn854xs365g")),
	})
	accs = append(accs, simtypes.Account{
		Address: sdk.AccAddress([]byte("sei1jdppe6fnj2q7hjsepty5crxtrryzhuqsjrj95y")),
	})

	// Test with account present
	addr1 := sdk.AccAddress([]byte("sei1qzdrwc3806zfdl98608nqnsvhg8hn854xs365g")).String()
	account, found := epochsimulation.FindAccount(accs, addr1)
	require.True(t, found)
	require.Equal(t, sdk.AccAddress([]byte("sei1qzdrwc3806zfdl98608nqnsvhg8hn854xs365g")), account.Address)

	// Test with account not present
	addr3 := sdk.AccAddress([]byte("address3")).String()
	account, found = epochsimulation.FindAccount(accs, addr3)
	require.False(t, found)
	require.Equal(t, simtypes.Account{}, account)

	// Test with invalid account address
	require.Panics(t, func() { epochsimulation.FindAccount(accs, "invalid") }, "The function did not panic with an invalid account address")
}
