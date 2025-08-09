package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

const StoreCodespace = "store"

var (
	ErrInvalidProof = sdkerrors.Register(StoreCodespace, 2, "invalid proof")
)
