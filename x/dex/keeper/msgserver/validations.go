package msgserver

import (
	"fmt"
	"math"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
)

// Since cosmwasm would amplify gas limit by a multiplier for its internal gas metering,
// we want to make sure the amplified result doesn't exceed uint64 limit.
func (k msgServer) ValidateRentBalance(rentBalance uint64) error {
	maxAllowedRent := k.maxAllowedRentBalance()
	if rentBalance > maxAllowedRent {
		return fmt.Errorf("maximum allowed rent balance is %d", maxAllowedRent)
	}
	return nil
}

func (k msgServer) maxAllowedRentBalance() uint64 {
	// TODO: replace with a wasm keeper query once its gas registry is made public
	return uint64(math.MaxUint64) / wasmkeeper.DefaultGasMultiplier
}
