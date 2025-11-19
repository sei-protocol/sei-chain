package exported

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// GenesisBalance defines a genesis balance interface that allows for account
// address and balance retrieval.
type GenesisBalance interface {
	GetAddress() seitypes.AccAddress
	GetCoins() sdk.Coins
}
