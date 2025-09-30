package types

import (
	errorsmod "cosmossdk.io/errors"
)

var (
	ErrCovenantNotFound          = errorsmod.Register(ModuleName, 1100, "covenant not found")
	ErrCovenantUnauthorized      = errorsmod.Register(ModuleName, 1101, "executor not authorized for covenant")
	ErrCovenantPayeeMismatch     = errorsmod.Register(ModuleName, 1102, "payee does not match covenant")
	ErrCovenantInsufficientFunds = errorsmod.Register(ModuleName, 1103, "insufficient covenant balance")
)
