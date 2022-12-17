package accesscontrol

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

const (
	DefaultCodespace = types.ModuleName
)

var (
	ErrUnexpectedWasmDependency         = sdkerrors.Register(DefaultCodespace, 2, "unexpected wasm dependency detected")
	ErrWasmDependencyRegistrationFailed = sdkerrors.Register(DefaultCodespace, 3, "wasm dependency registration failed")
)
