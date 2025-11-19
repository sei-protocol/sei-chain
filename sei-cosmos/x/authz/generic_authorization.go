package authz

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

var (
	_ Authorization = &GenericAuthorization{}
)

// NewGenericAuthorization creates a new GenericAuthorization object.
func NewGenericAuthorization(msgTypeURL string) *GenericAuthorization {
	return &GenericAuthorization{
		Msg: msgTypeURL,
	}
}

// MsgTypeURL implements Authorization.MsgTypeURL.
func (a GenericAuthorization) MsgTypeURL() string {
	return a.Msg
}

// Accept implements Authorization.Accept.
func (a GenericAuthorization) Accept(ctx sdk.Context, msg seitypes.Msg) (AcceptResponse, error) {
	return AcceptResponse{Accept: true}, nil
}

// ValidateBasic implements Authorization.ValidateBasic.
func (a GenericAuthorization) ValidateBasic() error {
	return nil
}
