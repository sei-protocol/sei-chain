package bindings

import (
	sdkErrors "github.com/sei-protocol/sei-chain/cosmos-sdk/types/errors"
)

// Codes for wasm contract errors
var (
	DefaultCodespace = "wasmbinding"

	ErrParsingSeiWasmMsg = sdkErrors.Register(DefaultCodespace, 2, "Error parsing Sei Wasm Message")
)
