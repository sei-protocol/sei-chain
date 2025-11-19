package types

import (
	"bytes"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// staking message types
const (
	TypeMsgUndelegate      = "begin_unbonding"
	TypeMsgEditValidator   = "edit_validator"
	TypeMsgCreateValidator = "create_validator"
	TypeMsgDelegate        = "delegate"
	TypeMsgBeginRedelegate = "begin_redelegate"
)

var (
	_ seitypes.Msg                       = &MsgCreateValidator{}
	_ codectypes.UnpackInterfacesMessage = (*MsgCreateValidator)(nil)
	_ seitypes.Msg                       = &MsgCreateValidator{}
	_ seitypes.Msg                       = &MsgEditValidator{}
	_ seitypes.Msg                       = &MsgDelegate{}
	_ seitypes.Msg                       = &MsgUndelegate{}
	_ seitypes.Msg                       = &MsgBeginRedelegate{}
)

// NewMsgCreateValidator creates a new MsgCreateValidator instance.
// Delegator address and validator address are the same.
func NewMsgCreateValidator(
	valAddr seitypes.ValAddress, pubKey cryptotypes.PubKey, //nolint:interfacer
	selfDelegation sdk.Coin, description Description, commission CommissionRates, minSelfDelegation sdk.Int,
) (*MsgCreateValidator, error) {
	var pkAny *codectypes.Any
	if pubKey != nil {
		var err error
		if pkAny, err = codectypes.NewAnyWithValue(pubKey); err != nil {
			return nil, err
		}
	}
	return &MsgCreateValidator{
		Description:       description,
		DelegatorAddress:  seitypes.AccAddress(valAddr).String(),
		ValidatorAddress:  valAddr.String(),
		Pubkey:            pkAny,
		Value:             selfDelegation,
		Commission:        commission,
		MinSelfDelegation: minSelfDelegation,
	}, nil
}

// Route implements the seitypes.Msg interface.
func (msg MsgCreateValidator) Route() string { return RouterKey }

// Type implements the seitypes.Msg interface.
func (msg MsgCreateValidator) Type() string { return TypeMsgCreateValidator }

// GetSigners implements the seitypes.Msg interface. It returns the address(es) that
// must sign over msg.GetSignBytes().
// If the validator address is not same as delegator's, then the validator must
// sign the msg as well.
func (msg MsgCreateValidator) GetSigners() []seitypes.AccAddress {
	// delegator is first signer so delegator pays fees
	delAddr, err := seitypes.AccAddressFromBech32(msg.DelegatorAddress)
	if err != nil {
		panic(err)
	}
	addrs := []seitypes.AccAddress{delAddr}
	addr, err := seitypes.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(delAddr.Bytes(), addr.Bytes()) {
		addrs = append(addrs, seitypes.AccAddress(addr))
	}

	return addrs
}

// GetSignBytes returns the message bytes to sign over.
func (msg MsgCreateValidator) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the seitypes.Msg interface.
func (msg MsgCreateValidator) ValidateBasic() error {
	// note that unmarshaling from bech32 ensures either empty or valid
	delAddr, err := seitypes.AccAddressFromBech32(msg.DelegatorAddress)
	if err != nil {
		return err
	}
	if delAddr.Empty() {
		return ErrEmptyDelegatorAddr
	}

	if msg.ValidatorAddress == "" {
		return ErrEmptyValidatorAddr
	}

	valAddr, err := seitypes.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return err
	}
	if !seitypes.AccAddress(valAddr).Equals(delAddr) {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "validator address is invalid")
	}

	if msg.Pubkey == nil {
		return ErrEmptyValidatorPubKey
	}

	if !msg.Value.IsValid() || !msg.Value.Amount.IsPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid delegation amount")
	}

	if msg.Description == (Description{}) {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "empty description")
	}

	if msg.Commission == (CommissionRates{}) {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "empty commission")
	}

	if err := msg.Commission.Validate(); err != nil {
		return err
	}

	if !msg.MinSelfDelegation.IsPositive() {
		return sdkerrors.Wrap(
			sdkerrors.ErrInvalidRequest,
			"minimum self delegation must be a positive integer",
		)
	}

	if msg.Value.Amount.LT(msg.MinSelfDelegation) {
		return ErrSelfDelegationBelowMinimum
	}

	return nil
}

// UnpackInterfaces implements UnpackInterfacesMessage.UnpackInterfaces
func (msg MsgCreateValidator) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	var pubKey cryptotypes.PubKey
	return unpacker.UnpackAny(msg.Pubkey, &pubKey)
}

// NewMsgEditValidator creates a new MsgEditValidator instance
//
//nolint:interfacer
func NewMsgEditValidator(valAddr seitypes.ValAddress, description Description, newRate *sdk.Dec, newMinSelfDelegation *sdk.Int) *MsgEditValidator {
	return &MsgEditValidator{
		Description:       description,
		CommissionRate:    newRate,
		ValidatorAddress:  valAddr.String(),
		MinSelfDelegation: newMinSelfDelegation,
	}
}

// Route implements the seitypes.Msg interface.
func (msg MsgEditValidator) Route() string { return RouterKey }

// Type implements the seitypes.Msg interface.
func (msg MsgEditValidator) Type() string { return TypeMsgEditValidator }

// GetSigners implements the seitypes.Msg interface.
func (msg MsgEditValidator) GetSigners() []seitypes.AccAddress {
	valAddr, err := seitypes.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{valAddr.Bytes()}
}

// GetSignBytes implements the seitypes.Msg interface.
func (msg MsgEditValidator) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the seitypes.Msg interface.
func (msg MsgEditValidator) ValidateBasic() error {
	if msg.ValidatorAddress == "" {
		return ErrEmptyValidatorAddr
	}

	if msg.Description == (Description{}) {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "empty description")
	}

	if msg.MinSelfDelegation != nil && !msg.MinSelfDelegation.IsPositive() {
		return sdkerrors.Wrap(
			sdkerrors.ErrInvalidRequest,
			"minimum self delegation must be a positive integer",
		)
	}

	if msg.CommissionRate != nil {
		if msg.CommissionRate.GT(sdk.OneDec()) || msg.CommissionRate.IsNegative() {
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "commission rate must be between 0 and 1 (inclusive)")
		}
	}

	return nil
}

// NewMsgDelegate creates a new MsgDelegate instance.
//
//nolint:interfacer
func NewMsgDelegate(delAddr seitypes.AccAddress, valAddr seitypes.ValAddress, amount sdk.Coin) *MsgDelegate {
	return &MsgDelegate{
		DelegatorAddress: delAddr.String(),
		ValidatorAddress: valAddr.String(),
		Amount:           amount,
	}
}

// Route implements the seitypes.Msg interface.
func (msg MsgDelegate) Route() string { return RouterKey }

// Type implements the seitypes.Msg interface.
func (msg MsgDelegate) Type() string { return TypeMsgDelegate }

// GetSigners implements the seitypes.Msg interface.
func (msg MsgDelegate) GetSigners() []seitypes.AccAddress {
	delAddr, err := seitypes.AccAddressFromBech32(msg.DelegatorAddress)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{delAddr}
}

// GetSignBytes implements the seitypes.Msg interface.
func (msg MsgDelegate) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the seitypes.Msg interface.
func (msg MsgDelegate) ValidateBasic() error {
	if msg.DelegatorAddress == "" {
		return ErrEmptyDelegatorAddr
	}

	if msg.ValidatorAddress == "" {
		return ErrEmptyValidatorAddr
	}

	if !msg.Amount.IsValid() || !msg.Amount.Amount.IsPositive() {
		return sdkerrors.Wrap(
			sdkerrors.ErrInvalidRequest,
			"invalid delegation amount",
		)
	}

	return nil
}

// NewMsgBeginRedelegate creates a new MsgBeginRedelegate instance.
//
//nolint:interfacer
func NewMsgBeginRedelegate(
	delAddr seitypes.AccAddress, valSrcAddr, valDstAddr seitypes.ValAddress, amount sdk.Coin,
) *MsgBeginRedelegate {
	return &MsgBeginRedelegate{
		DelegatorAddress:    delAddr.String(),
		ValidatorSrcAddress: valSrcAddr.String(),
		ValidatorDstAddress: valDstAddr.String(),
		Amount:              amount,
	}
}

// Route implements the seitypes.Msg interface.
func (msg MsgBeginRedelegate) Route() string { return RouterKey }

// Type implements the seitypes.Msg interface
func (msg MsgBeginRedelegate) Type() string { return TypeMsgBeginRedelegate }

// GetSigners implements the seitypes.Msg interface
func (msg MsgBeginRedelegate) GetSigners() []seitypes.AccAddress {
	delAddr, err := seitypes.AccAddressFromBech32(msg.DelegatorAddress)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{delAddr}
}

// GetSignBytes implements the seitypes.Msg interface.
func (msg MsgBeginRedelegate) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the seitypes.Msg interface.
func (msg MsgBeginRedelegate) ValidateBasic() error {
	if msg.DelegatorAddress == "" {
		return ErrEmptyDelegatorAddr
	}

	if msg.ValidatorSrcAddress == "" {
		return ErrEmptyValidatorAddr
	}

	if msg.ValidatorDstAddress == "" {
		return ErrEmptyValidatorAddr
	}

	if !msg.Amount.IsValid() || !msg.Amount.Amount.IsPositive() {
		return sdkerrors.Wrap(
			sdkerrors.ErrInvalidRequest,
			"invalid shares amount",
		)
	}

	return nil
}

// NewMsgUndelegate creates a new MsgUndelegate instance.
//
//nolint:interfacer
func NewMsgUndelegate(delAddr seitypes.AccAddress, valAddr seitypes.ValAddress, amount sdk.Coin) *MsgUndelegate {
	return &MsgUndelegate{
		DelegatorAddress: delAddr.String(),
		ValidatorAddress: valAddr.String(),
		Amount:           amount,
	}
}

// Route implements the seitypes.Msg interface.
func (msg MsgUndelegate) Route() string { return RouterKey }

// Type implements the seitypes.Msg interface.
func (msg MsgUndelegate) Type() string { return TypeMsgUndelegate }

// GetSigners implements the seitypes.Msg interface.
func (msg MsgUndelegate) GetSigners() []seitypes.AccAddress {
	delAddr, err := seitypes.AccAddressFromBech32(msg.DelegatorAddress)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{delAddr}
}

// GetSignBytes implements the seitypes.Msg interface.
func (msg MsgUndelegate) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the seitypes.Msg interface.
func (msg MsgUndelegate) ValidateBasic() error {
	if msg.DelegatorAddress == "" {
		return ErrEmptyDelegatorAddr
	}

	if msg.ValidatorAddress == "" {
		return ErrEmptyValidatorAddr
	}

	if !msg.Amount.IsValid() || !msg.Amount.Amount.IsPositive() {
		return sdkerrors.Wrap(
			sdkerrors.ErrInvalidRequest,
			"invalid shares amount",
		)
	}

	return nil
}
