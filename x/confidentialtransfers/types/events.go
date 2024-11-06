package types

// Minting module event types
const (
	EventTypeInitializeAccount   = "initialize_account"
	EventTypeDeposit             = "deposit"
	EventTypeWithdraw            = "withdraw"
	EventTypeApplyPendingBalance = "apply_pending_balance"
	EventTypeTransfer            = "transfer"
	EventTypeCloseAccount        = "close_account"

	AttributeDenom   = "denom"
	AttributeAddress = "address"
)
