package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/cosmos/types/errors"
)

// ICA Controller sentinel errors
var (
	ErrControllerSubModuleDisabled = sdkerrors.Register(SubModuleName, 2, "controller submodule is disabled")
)
