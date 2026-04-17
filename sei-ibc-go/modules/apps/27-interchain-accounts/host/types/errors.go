package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// ICA Host sentinel errors
var (
	ErrHostSubModuleDisabled = sdkerrors.Register(SubModuleName, 2, "host submodule is disabled")
)
