package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// x/nitro module sentinel errors
var (
	ErrValidateMerkleProof     = sdkerrors.Register(ModuleName, 2, "Error validating merkle proof: hash and direction sizes are not the same")
	ErrInvalidMerkleProof      = sdkerrors.Register(ModuleName, 3, "Error invalid merkle proof")
	ErrInvalidAccountState     = sdkerrors.Register(ModuleName, 4, "Error invalid provided account state")
	ErrInvalidFraudStatePubkey = sdkerrors.Register(ModuleName, 6, "Error invalid provided fraud state public key")
)
