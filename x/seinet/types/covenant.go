package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Covenant represents an active payword covenant that can be settled.
type Covenant struct {
	Id        string    `json:"id" yaml:"id"`
	Creator   string    `json:"creator" yaml:"creator"`
	Payee     string    `json:"payee" yaml:"payee"`
	AmountDue sdk.Coins `json:"amount_due" yaml:"amount_due"`
}

// ValidateBasic performs stateless validation of the covenant data.
func (c Covenant) ValidateBasic() error {
	if c.Id == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "covenant id cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(c.Creator); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address: %s", err)
	}

	if _, err := sdk.AccAddressFromBech32(c.Payee); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid payee address: %s", err)
	}

	if !c.AmountDue.IsAllPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "amount due must be positive")
	}

	return nil
}
