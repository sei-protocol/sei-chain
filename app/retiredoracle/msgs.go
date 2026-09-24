// Package retiredoracle provides decode-only compatibility for retired oracle transactions.
package retiredoracle

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

const (
	TypeMsgDelegateFeedConsent       = "delegate_feeder"
	TypeMsgAggregateExchangeRateVote = "aggregate_exchange_rate_vote"
)

var (
	_ sdk.Msg = (*MsgDelegateFeedConsent)(nil)
	_ sdk.Msg = (*MsgAggregateExchangeRateVote)(nil)
)

func NewMsgAggregateExchangeRateVote(exchangeRates string, feeder sdk.AccAddress, validator sdk.ValAddress) *MsgAggregateExchangeRateVote {
	return &MsgAggregateExchangeRateVote{
		ExchangeRates: exchangeRates,
		Feeder:        feeder.String(),
		Validator:     validator.String(),
	}
}

func (msg MsgAggregateExchangeRateVote) Route() string { return "oracle" }

func (msg MsgAggregateExchangeRateVote) Type() string { return TypeMsgAggregateExchangeRateVote }

func (msg MsgAggregateExchangeRateVote) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

func (msg MsgAggregateExchangeRateVote) GetSigners() []sdk.AccAddress {
	feeder, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{feeder}
}

func (msg MsgAggregateExchangeRateVote) ValidateBasic() error { return ErrDeprecated }

func NewMsgDelegateFeedConsent(operatorAddress sdk.ValAddress, feederAddress sdk.AccAddress) *MsgDelegateFeedConsent {
	return &MsgDelegateFeedConsent{
		Operator: operatorAddress.String(),
		Delegate: feederAddress.String(),
	}
}

func (msg MsgDelegateFeedConsent) Route() string { return "oracle" }

func (msg MsgDelegateFeedConsent) Type() string { return TypeMsgDelegateFeedConsent }

func (msg MsgDelegateFeedConsent) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

func (msg MsgDelegateFeedConsent) GetSigners() []sdk.AccAddress {
	operator, err := sdk.ValAddressFromBech32(msg.Operator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sdk.AccAddress(operator)}
}

func (msg MsgDelegateFeedConsent) ValidateBasic() error { return ErrDeprecated }
