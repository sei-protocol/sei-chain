package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// x/dex module sentinel errors
var (
	ErrEncodeDexPlaceOrders       = sdkerrors.Register(ModuleName, 2, "Error while encoding dex order placement msg in wasmd")
	ErrEncodeDexCancelOrders      = sdkerrors.Register(ModuleName, 3, "Error while encoding dex order cancellation msg in wasmd")
	ErrParsingSeiDexQuery         = sdkerrors.Register(ModuleName, 4, "Error parsing SeiDexQuery")
	ErrEncodingDexTwaps           = sdkerrors.Register(ModuleName, 6, "Error encoding dex twaps as JSON")
	ErrEncodingOrders             = sdkerrors.Register(ModuleName, 8, "Error encoding orders as JSON")
	ErrEncodingOrder              = sdkerrors.Register(ModuleName, 10, "Error encoding order as JSON")
	ErrEncodingOrderSimulation    = sdkerrors.Register(ModuleName, 12, "Error encoding order simulation as JSON")
	ErrInvalidOrderID             = sdkerrors.Register(ModuleName, 13, "Error order id not found")
	ErrEncodingLatestPrice        = sdkerrors.Register(ModuleName, 14, "Error encoding latest price as JSON")
	ErrUnknownSeiDexQuery         = sdkerrors.Register(ModuleName, 15, "Error unknown sei dex query")
	ErrPairNotRegistered          = sdkerrors.Register(ModuleName, 16, "pair is not registered")
	ErrContractNotExists          = sdkerrors.Register(ModuleName, 17, "Error finding contract info")
	ErrParsingContractInfo        = sdkerrors.Register(ModuleName, 18, "Error parsing contract info")
	ErrInsufficientRent           = sdkerrors.Register(ModuleName, 19, "Error contract does not have sufficient fee")
	ErrCircularContractDependency = sdkerrors.Register(ModuleName, 1103, "circular contract dependency detected")
	ErrContractSuspended          = sdkerrors.Register(ModuleName, 1104, "contract suspended")
	ErrContractNotSuspended       = sdkerrors.Register(ModuleName, 1105, "contract not suspended")
)
