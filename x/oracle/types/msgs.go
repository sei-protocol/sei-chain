package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ensure Msg interface compliance at compile time
var (
	_ seitypes.Msg = &MsgDelegateFeedConsent{}
	_ seitypes.Msg = &MsgAggregateExchangeRateVote{}
)

// oracle message types
const (
	TypeMsgDelegateFeedConsent       = "delegate_feeder"
	TypeMsgAggregateExchangeRateVote = "aggregate_exchange_rate_vote"
)

//-------------------------------------------------
//-------------------------------------------------

// NewMsgAggregateExchangeRateVote returns MsgAggregateExchangeRateVote instance
func NewMsgAggregateExchangeRateVote(exchangeRates string, feeder seitypes.AccAddress, validator seitypes.ValAddress) *MsgAggregateExchangeRateVote {
	return &MsgAggregateExchangeRateVote{
		ExchangeRates: exchangeRates,
		Feeder:        feeder.String(),
		Validator:     validator.String(),
	}
}

// Route implements seitypes.Msg
func (msg MsgAggregateExchangeRateVote) Route() string { return RouterKey }

// Type implements seitypes.Msg
func (msg MsgAggregateExchangeRateVote) Type() string { return TypeMsgAggregateExchangeRateVote }

// GetSignBytes implements seitypes.Msg
func (msg MsgAggregateExchangeRateVote) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners implements seitypes.Msg
func (msg MsgAggregateExchangeRateVote) GetSigners() []seitypes.AccAddress {
	feeder, err := seitypes.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		panic(err)
	}

	return []seitypes.AccAddress{feeder}
}

// ValidateBasic implements seitypes.Msg
func (msg MsgAggregateExchangeRateVote) ValidateBasic() error {
	_, err := seitypes.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid feeder address (%s)", err)
	}

	_, err = sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid operator address (%s)", err)
	}

	if l := len(msg.ExchangeRates); l == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, "must provide at least one oracle exchange rate")
	} else if l > 4096 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "exchange rates string can not exceed 4096 characters")
	}

	exchangeRates, err := ParseExchangeRateTuples(msg.ExchangeRates)
	if err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "failed to parse exchange rates string cause: "+err.Error())
	}

	for _, exchangeRate := range exchangeRates {
		// Check overflow bit length
		if exchangeRate.ExchangeRate.BigInt().BitLen() > 255+sdk.DecimalPrecisionBits {
			return sdkerrors.Wrap(ErrInvalidExchangeRate, "overflow")
		}
	}
	return nil
}

// NewMsgDelegateFeedConsent creates a MsgDelegateFeedConsent instance
func NewMsgDelegateFeedConsent(operatorAddress seitypes.ValAddress, feederAddress seitypes.AccAddress) *MsgDelegateFeedConsent {
	return &MsgDelegateFeedConsent{
		Operator: operatorAddress.String(),
		Delegate: feederAddress.String(),
	}
}

// Route implements seitypes.Msg
func (msg MsgDelegateFeedConsent) Route() string { return RouterKey }

// Type implements seitypes.Msg
func (msg MsgDelegateFeedConsent) Type() string { return TypeMsgDelegateFeedConsent }

// GetSignBytes implements seitypes.Msg
func (msg MsgDelegateFeedConsent) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners implements seitypes.Msg
func (msg MsgDelegateFeedConsent) GetSigners() []seitypes.AccAddress {
	operator, err := sdk.ValAddressFromBech32(msg.Operator)
	if err != nil {
		panic(err)
	}

	return []seitypes.AccAddress{seitypes.AccAddress(operator)}
}

// ValidateBasic implements seitypes.Msg
func (msg MsgDelegateFeedConsent) ValidateBasic() error {
	_, err := sdk.ValAddressFromBech32(msg.Operator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid operator address (%s)", err)
	}

	_, err = seitypes.AccAddressFromBech32(msg.Delegate)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid delegate address (%s)", err)
	}

	return nil
}
