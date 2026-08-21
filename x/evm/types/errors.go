package types

import (
	"fmt"
	"strings"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// ErrAssociateDeprecated is returned by the MsgAssociate handler.
var ErrAssociateDeprecated = sdkerrors.Register(ModuleName, 2, "MsgAssociate is deprecated")

type AssociationMissingErr struct {
	Address string
}

func NewAssociationMissingErr(address string) AssociationMissingErr {
	return AssociationMissingErr{Address: address}
}

func (e AssociationMissingErr) Error() string {
	return fmt.Sprintf("address %s is not linked", e.Address)
}

func (e AssociationMissingErr) AddressType() string {
	if strings.HasPrefix(e.Address, "0x") {
		return "evm"
	}
	return "sei"
}
