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
	ErrGettingDexTwaps            = sdkerrors.Register(ModuleName, 5, "Error while getting dex Twaps")
	ErrEncodingDexTwaps           = sdkerrors.Register(ModuleName, 6, "Error encoding dex twaps as JSON")
	ErrGettingOrders              = sdkerrors.Register(ModuleName, 7, "Error while getting orders")
	ErrEncodingOrders             = sdkerrors.Register(ModuleName, 8, "Error encoding orders as JSON")
	ErrGettingOrderByID           = sdkerrors.Register(ModuleName, 9, "Error while getting order by ID")
	ErrEncodingOrder              = sdkerrors.Register(ModuleName, 10, "Error encoding order as JSON")
	ErrGettingOrderSimulation     = sdkerrors.Register(ModuleName, 11, "Error while getting order simulation")
	ErrEncodingOrderSimulation    = sdkerrors.Register(ModuleName, 12, "Error encoding order simulation as JSON")
	ErrInvalidOrderID             = sdkerrors.Register(ModuleName, 13, "Error order id not found")
	ErrEncodingLatestPrice        = sdkerrors.Register(ModuleName, 14, "Error encoding latest price as JSON")
	ErrUnknownSeiDexQuery         = sdkerrors.Register(ModuleName, 15, "Error unknown sei dex query")
	ErrPairNotRegistered          = sdkerrors.Register(ModuleName, 16, "pair is not registered")
	ErrSample                     = sdkerrors.Register(ModuleName, 1100, "sample error")
	InsufficientAssetError        = sdkerrors.Register(ModuleName, 1101, "insufficient fund")
	UnknownCustomMessageError     = sdkerrors.Register(ModuleName, 1102, "unknown custom message")
	ErrCircularContractDependency = sdkerrors.Register(ModuleName, 1103, "circular contract dependency detected")
)
