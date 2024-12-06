package tracers

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

type EVMKeeper interface {
	GetEVMAddressOrDefault(sdk.Context, sdk.AccAddress) common.Address
}

// GetEVMAddress is a thin wrapper around GetEVMAddressOrDefault in the EVMKeeper interface
// with the important differences that:
// - It does **not** bill gas as this operation is for tracing purposes
// - It also returns the default EVM address if the mapping does not exist
func GetEVMAddress(ctx sdk.Context, keeper EVMKeeper, address sdk.AccAddress) common.Address {
	noGasBillingCtx := ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())

	return keeper.GetEVMAddressOrDefault(noGasBillingCtx, address)
}
