package msgserver

import (
	"fmt"
	"math"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// Since cosmwasm would amplify gas limit by a multiplier for its internal gas metering,
// we want to make sure the amplified result doesn't exceed uint64 limit.
func (k msgServer) ValidateRentBalance(ctx sdk.Context, rentBalance uint64) error {
	maxAllowedRent := k.maxAllowedRentBalance()
	minAllowedRent := k.minAllowedRentBalance(ctx)
	if rentBalance > maxAllowedRent || rentBalance < minAllowedRent {
		return fmt.Errorf("rent balance %d is either bigger than the maximum allowed rent balance %d, or smaller than the minimal allowed balance %d",
			rentBalance, maxAllowedRent, minAllowedRent)
	}
	return nil
}

func (k msgServer) ValidateSuspension(ctx sdk.Context, contractAddress string) error {
	contract, err := k.GetContract(ctx, contractAddress)
	if err == nil && contract.Suspended {
		return types.ErrContractSuspended
	}
	return nil
}

func (k msgServer) maxAllowedRentBalance() uint64 {
	// TODO: replace with a wasm keeper query once its gas registry is made public
	return uint64(math.MaxUint64) / wasmkeeper.DefaultGasMultiplier
}

func (k msgServer) minAllowedRentBalance(ctx sdk.Context) uint64 {
	params := k.GetParams(ctx)
	return params.MinRentDeposit
}
