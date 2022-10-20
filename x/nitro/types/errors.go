package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// x/nitro module sentinel errors
var (
	ErrValidateMerkleProof  = sdkerrors.Register(ModuleName, 2, "Error validating merkle proof: hash and direction sizes are not the same")
	ErrInvalidateMerkleProof         = sdkerrors.Register(ModuleName, 3, "Error invalid merkle proof")
)
