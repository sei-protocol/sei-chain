package keeper

import (
	"strings"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

const portIDPrefix = "wasm."

func PortIDForContract(addr sdk.AccAddress) string {
	return portIDPrefix + addr.String()
}

func ContractFromPortID(portID string) (sdk.AccAddress, error) {
	if !strings.HasPrefix(portID, portIDPrefix) {
		return nil, sdkerrors.Wrapf(types.ErrInvalid, "without prefix")
	}
	return sdk.AccAddressFromBech32(portID[len(portIDPrefix):])
}
