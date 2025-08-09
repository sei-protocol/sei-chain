package wasmtesting

import sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

type MockCoinTransferrer struct {
	TransferCoinsFn func(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
}

func (m *MockCoinTransferrer) TransferCoins(ctx sdk.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.TransferCoinsFn == nil {
		panic("not expected to be called")
	}
	return m.TransferCoinsFn(ctx, fromAddr, toAddr, amt)
}
