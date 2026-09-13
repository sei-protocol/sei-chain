package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

const StoreCodespace = "store"

var (
	ErrInvalidProof = sdkerrors.Register(StoreCodespace, 2, "invalid proof")

	// ErrSubspaceCapExceeded is returned when a /subspace scan would exceed configured limits.
	ErrSubspaceCapExceeded = sdkerrors.Register(StoreCodespace, 3, "subspace result exceeds limit")
)
