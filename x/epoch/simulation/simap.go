package simulation

import (
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// FindAccount find a specific address from an account list
func FindAccount(accs []simtypes.Account, address string) (simtypes.Account, bool) {
	creator, err := seitypes.AccAddressFromBech32(address)
	if err != nil {
		panic(err)
	}
	return simtypes.FindAccount(accs, creator)
}
