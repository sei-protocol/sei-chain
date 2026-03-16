package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/cosmos/types/errors"
)

// Localhost sentinel errors
var (
	ErrConsensusStatesNotStored = sdkerrors.Register(SubModuleName, 2, "localhost does not store consensus states")
)
