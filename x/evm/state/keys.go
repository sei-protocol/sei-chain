package state

/*
*
Transient Module State Keys
*/
var (
	// Represents the sum of all unassociated evm account balances
	// If evm module balance is higher than this value at the end of
	// the transaction, we need to burn from module balance in order
	// for this number to align.
	TotalUnassociatedBalanceKey = []byte{0x01}
	GasRefundKey                = []byte{0x02}
	LogsKey                     = []byte{0x03}
	AccessListKey               = []byte{0x04}
)

/*
*
Transient Account State Keys
*/
var (
	AccountCreated = []byte{0x01}
	AccountDeleted = []byte{0x02}
)
