package types

import (
	"github.com/tendermint/tendermint/crypto/tmhash"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// ensure Msg interface compliance at compile time
var (
	_ sdk.Msg = &MsgDelegateFeedConsent{}
	_ sdk.Msg = &MsgAggregateExchangeRatePrevote{}
	_ sdk.Msg = &MsgAggregateExchangeRateVote{}
	_ sdk.Msg = &MsgAggregateExchangeRateCombinedVote{}
)

// oracle message types
const (
	TypeMsgDelegateFeedConsent               = "delegate_feeder"
	TypeMsgAggregateExchangeRatePrevote      = "aggregate_exchange_rate_prevote"
	TypeMsgAggregateExchangeRateVote         = "aggregate_exchange_rate_vote"
	TypeMsgAggregateExchangeRateCombinedVote = "aggregate_exchange_rate_combined_vote"
)

//-------------------------------------------------
//-------------------------------------------------

// NewMsgAggregateExchangeRatePrevote returns MsgAggregateExchangeRatePrevote instance
func NewMsgAggregateExchangeRatePrevote(hash AggregateVoteHash, feeder sdk.AccAddress, validator sdk.ValAddress) *MsgAggregateExchangeRatePrevote {
	return &MsgAggregateExchangeRatePrevote{
		Hash:      hash.String(),
		Feeder:    feeder.String(),
		Validator: validator.String(),
	}
}

// Route implements sdk.Msg
func (msg MsgAggregateExchangeRatePrevote) Route() string { return RouterKey }

// Type implements sdk.Msg
func (msg MsgAggregateExchangeRatePrevote) Type() string { return TypeMsgAggregateExchangeRatePrevote }

// GetSignBytes implements sdk.Msg
func (msg MsgAggregateExchangeRatePrevote) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners implements sdk.Msg
func (msg MsgAggregateExchangeRatePrevote) GetSigners() []sdk.AccAddress {
	feeder, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		panic(err)
	}

	return []sdk.AccAddress{feeder}
}

// ValidateBasic Implements sdk.Msg
func (msg MsgAggregateExchangeRatePrevote) ValidateBasic() error {
	_, err := AggregateVoteHashFromHexString(msg.Hash)
	if err != nil {
		return sdkerrors.Wrapf(ErrInvalidHash, "Invalid vote hash (%s)", err)
	}

	// HEX encoding doubles the hash length
	if len(msg.Hash) != tmhash.TruncatedSize*2 {
		return ErrInvalidHashLength
	}

	_, err = sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid feeder address (%s)", err)
	}

	_, err = sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid operator address (%s)", err)
	}

	return nil
}

// NewMsgAggregateExchangeRateVote returns MsgAggregateExchangeRateVote instance
func NewMsgAggregateExchangeRateVote(salt string, exchangeRates string, feeder sdk.AccAddress, validator sdk.ValAddress) *MsgAggregateExchangeRateVote {
	return &MsgAggregateExchangeRateVote{
		Salt:          salt,
		ExchangeRates: exchangeRates,
		Feeder:        feeder.String(),
		Validator:     validator.String(),
	}
}

// Route implements sdk.Msg
func (msg MsgAggregateExchangeRateVote) Route() string { return RouterKey }

// Type implements sdk.Msg
func (msg MsgAggregateExchangeRateVote) Type() string { return TypeMsgAggregateExchangeRateVote }

// GetSignBytes implements sdk.Msg
func (msg MsgAggregateExchangeRateVote) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners implements sdk.Msg
func (msg MsgAggregateExchangeRateVote) GetSigners() []sdk.AccAddress {
	feeder, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		panic(err)
	}

	return []sdk.AccAddress{feeder}
}

// ValidateBasic implements sdk.Msg
func (msg MsgAggregateExchangeRateVote) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Feeder)
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

	if len(msg.Salt) > 4 || len(msg.Salt) < 1 {
		return sdkerrors.Wrap(ErrInvalidSaltLength, "salt length must be [1, 4]")
	}

	return nil
}

// NewMsgAggregateExchangeRatePrevote returns MsgAggregateExchangeRatePrevote instance
func NewMsgAggregateExchangeRateCombinedVote(voteSalt string, voteExchangeRates string, prevoteHash AggregateVoteHash, feeder sdk.AccAddress, validator sdk.ValAddress) *MsgAggregateExchangeRateCombinedVote {
	return &MsgAggregateExchangeRateCombinedVote{
		VoteSalt:          voteSalt,
		VoteExchangeRates: voteExchangeRates,
		PrevoteHash:       prevoteHash.String(),
		Feeder:            feeder.String(),
		Validator:         validator.String(),
	}
}

// Route implements sdk.Msg
func (msg MsgAggregateExchangeRateCombinedVote) Route() string { return RouterKey }

// Type implements sdk.Msg
func (msg MsgAggregateExchangeRateCombinedVote) Type() string {
	return TypeMsgAggregateExchangeRateCombinedVote
}

// GetSignBytes implements sdk.Msg
func (msg MsgAggregateExchangeRateCombinedVote) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners implements sdk.Msg
func (msg MsgAggregateExchangeRateCombinedVote) GetSigners() []sdk.AccAddress {
	feeder, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		panic(err)
	}

	return []sdk.AccAddress{feeder}
}

func (msg MsgAggregateExchangeRateCombinedVote) GetVoteFromCombinedVote() *MsgAggregateExchangeRateVote {
	return &MsgAggregateExchangeRateVote{
		Salt:          msg.VoteSalt,
		ExchangeRates: msg.VoteExchangeRates,
		Feeder:        msg.Feeder,
		Validator:     msg.Validator,
	}
}

func (msg MsgAggregateExchangeRateCombinedVote) GetPrevoteFromCombinedVote() *MsgAggregateExchangeRatePrevote {
	return &MsgAggregateExchangeRatePrevote{
		Hash:      msg.PrevoteHash,
		Feeder:    msg.Feeder,
		Validator: msg.Validator,
	}
}

// ValidateBasic Implements sdk.Msg
func (msg MsgAggregateExchangeRateCombinedVote) ValidateBasic() error {
	// validate feeder + validator
	_, err := sdk.AccAddressFromBech32(msg.Feeder)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid feeder address (%s)", err)
	}

	_, err = sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid operator address (%s)", err)
	}

	// validate the vote info
	if l := len(msg.VoteExchangeRates); l == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, "must provide at least one oracle exchange rate")
	} else if l > 4096 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "exchange rates string can not exceed 4096 characters")
	}

	voteExchangeRates, err := ParseExchangeRateTuples(msg.VoteExchangeRates)
	if err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "failed to parse exchange rates string cause: "+err.Error())
	}

	for _, exchangeRate := range voteExchangeRates {
		// Check overflow bit length
		if exchangeRate.ExchangeRate.BigInt().BitLen() > 255+sdk.DecimalPrecisionBits {
			return sdkerrors.Wrap(ErrInvalidExchangeRate, "overflow")
		}
	}

	if len(msg.VoteSalt) > 4 || len(msg.VoteSalt) < 1 {
		return sdkerrors.Wrap(ErrInvalidSaltLength, "salt length must be [1, 4]")
	}

	// validate the prevote hash
	_, err = AggregateVoteHashFromHexString(msg.PrevoteHash)
	if err != nil {
		return sdkerrors.Wrapf(ErrInvalidHash, "Invalid vote hash (%s)", err)
	}

	// HEX encoding doubles the hash length
	if len(msg.PrevoteHash) != tmhash.TruncatedSize*2 {
		return ErrInvalidHashLength
	}

	return nil
}

// NewMsgDelegateFeedConsent creates a MsgDelegateFeedConsent instance
func NewMsgDelegateFeedConsent(operatorAddress sdk.ValAddress, feederAddress sdk.AccAddress) *MsgDelegateFeedConsent {
	return &MsgDelegateFeedConsent{
		Operator: operatorAddress.String(),
		Delegate: feederAddress.String(),
	}
}

// Route implements sdk.Msg
func (msg MsgDelegateFeedConsent) Route() string { return RouterKey }

// Type implements sdk.Msg
func (msg MsgDelegateFeedConsent) Type() string { return TypeMsgDelegateFeedConsent }

// GetSignBytes implements sdk.Msg
func (msg MsgDelegateFeedConsent) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

// GetSigners implements sdk.Msg
func (msg MsgDelegateFeedConsent) GetSigners() []sdk.AccAddress {
	operator, err := sdk.ValAddressFromBech32(msg.Operator)
	if err != nil {
		panic(err)
	}

	return []sdk.AccAddress{sdk.AccAddress(operator)}
}

// ValidateBasic implements sdk.Msg
func (msg MsgDelegateFeedConsent) ValidateBasic() error {
	_, err := sdk.ValAddressFromBech32(msg.Operator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid operator address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(msg.Delegate)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid delegate address (%s)", err)
	}

	return nil
}
