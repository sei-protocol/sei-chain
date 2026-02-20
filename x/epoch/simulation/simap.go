package simulation

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	simtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/simulation"
)

// FindAccount find a specific address from an account list
func FindAccount(accs []simtypes.Account, address string) (simtypes.Account, bool) {
	creator, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		panic(err)
	}
	return simtypes.FindAccount(accs, creator)
}
