package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// bank module event types
const (
	EventTypeTransfer    = "transfer"
	EventTypeWeiTransfer = "wei_transfer"

	AttributeKeyRecipient = "recipient"
	AttributeKeySender    = "sender"

	AttributeValueCategory = ModuleName

	// supply and balance tracking events name and attributes
	EventTypeCoinSpent    = "coin_spent"
	EventTypeCoinReceived = "coin_received"
	EventTypeWeiSpent     = "wei_spent"
	EventTypeWeiReceived  = "wei_received"
	EventTypeCoinMint     = "coinbase" // NOTE(fdymylja): using mint clashes with mint module event
	EventTypeCoinBurn     = "burn"

	AttributeKeySpender  = "spender"
	AttributeKeyReceiver = "receiver"
	AttributeKeyMinter   = "minter"
	AttributeKeyBurner   = "burner"
)

// NewCoinSpentEvent constructs a new coin spent sdk.Event
// nolint: interfacer
func NewCoinSpentEvent(spender sdk.AccAddress, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypeCoinSpent,
		sdk.NewAttribute(AttributeKeySpender, spender.String()),
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
	)
}

// NewCoinReceivedEvent constructs a new coin received sdk.Event
// nolint: interfacer
func NewCoinReceivedEvent(receiver sdk.AccAddress, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypeCoinReceived,
		sdk.NewAttribute(AttributeKeyReceiver, receiver.String()),
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
	)
}

// NewWeiSpentEvent constructs a new wei spent sdk.Event
// nolint: interfacer
func NewWeiSpentEvent(spender sdk.AccAddress, amount sdk.Int) sdk.Event {
	return sdk.NewEvent(
		EventTypeWeiSpent,
		sdk.NewAttribute(AttributeKeySpender, spender.String()),
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
	)
}

// NewWeiReceivedEvent constructs a new wei received sdk.Event
// nolint: interfacer
func NewWeiReceivedEvent(receiver sdk.AccAddress, amount sdk.Int) sdk.Event {
	return sdk.NewEvent(
		EventTypeWeiReceived,
		sdk.NewAttribute(AttributeKeyReceiver, receiver.String()),
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
	)
}

// NewCoinMintEvent construct a new coin minted sdk.Event
// nolint: interfacer
func NewCoinMintEvent(minter sdk.AccAddress, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypeCoinMint,
		sdk.NewAttribute(AttributeKeyMinter, minter.String()),
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
	)
}

// NewCoinBurnEvent constructs a new coin burned sdk.Event
// nolint: interfacer
func NewCoinBurnEvent(burner sdk.AccAddress, amount sdk.Coins) sdk.Event {
	return sdk.NewEvent(
		EventTypeCoinBurn,
		sdk.NewAttribute(AttributeKeyBurner, burner.String()),
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
	)
}
