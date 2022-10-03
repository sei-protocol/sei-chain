package types

// DONTCOVER

import (
	fmt "fmt"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// x/tokenfactory module sentinel errors
var (
	ErrDenomExists                        = sdkerrors.Register(ModuleName, 2, "attempting to create a denom that already exists (has bank metadata)")
	ErrUnauthorized                       = sdkerrors.Register(ModuleName, 3, "unauthorized account")
	ErrInvalidDenom                       = sdkerrors.Register(ModuleName, 4, "invalid denom")
	ErrInvalidCreator                     = sdkerrors.Register(ModuleName, 5, "invalid creator")
	ErrInvalidAuthorityMetadata           = sdkerrors.Register(ModuleName, 6, "invalid authority metadata")
	ErrInvalidGenesis                     = sdkerrors.Register(ModuleName, 7, "invalid genesis")
	ErrSubdenomTooLong                    = sdkerrors.Register(ModuleName, 8, fmt.Sprintf("subdenom too long, max length is %d bytes", MaxSubdenomLength))
	ErrCreatorTooLong                     = sdkerrors.Register(ModuleName, 9, fmt.Sprintf("creator too long, max length is %d bytes", MaxCreatorLength))
	ErrDenomDoesNotExist                  = sdkerrors.Register(ModuleName, 10, "denom does not exist")
	ErrEncodeTokenFactoryCreateDenom      = sdkerrors.Register(ModuleName, 11, "Error while encoding tokenfactory create denom msg in wasmd")
	ErrEncodeTokenFactoryMint             = sdkerrors.Register(ModuleName, 12, "Error while encoding tokenfactory mint denom msg in wasmd")
	ErrEncodeTokenFactoryBurn             = sdkerrors.Register(ModuleName, 13, "Error while encoding tokenfactory burn denom msg in wasmd")
	ErrEncodeTokenFactoryChangeAdmin      = sdkerrors.Register(ModuleName, 14, "Error while encoding tokenfactory change admin msg in wasmd")
	ErrParsingSeiTokenFactoryQuery        = sdkerrors.Register(ModuleName, 15, "Error parsing SeiTokenFactoryQuery")
	ErrQueryingCreatorInDenomFeeWhitelist = sdkerrors.Register(ModuleName, 16, "Error while querying whether creator in denom fee whitelist")
	ErrEncodingDenomFeeWhitelist          = sdkerrors.Register(ModuleName, 17, "Error encoding whitelist membership as JSON")
	ErrGettingDenomCreationFeeWhitelist   = sdkerrors.Register(ModuleName, 18, "Error while querying create denom fee whitelist")
	ErrEncodingDenomCreationFeeWhitelist  = sdkerrors.Register(ModuleName, 19, "Error encoding denom creation fee whitelist as JSON")
	ErrUnknownSeiTokenFactoryQuery        = sdkerrors.Register(ModuleName, 23, "Error unknown sei token factory query")
)
