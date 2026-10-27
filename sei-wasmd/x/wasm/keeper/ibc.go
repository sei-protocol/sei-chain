package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

const portIDPrefix = "wasm."

func PortIDForContract(addr sdk.AccAddress) string {
	return portIDPrefix + addr.String()
}
