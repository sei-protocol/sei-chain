package state

/*
*
Transient Module State Keys
*/
var (
	// changes to EVM module balance because of balance movements. If this value
	// does not equal to the change in EVM module account balance minus the minted
	// amount at the end of the execution, the transaction should fail.
	DeficitKey = []byte{0x01}
	// the number of base tokens minted to temporarily facilitate balance movements.
	// At the end of execution, `minted` number of base tokens will be burnt.
	MintedKey     = []byte{0x02}
	GasRefundKey  = []byte{0x03}
	LogsKey       = []byte{0x04}
	AccessListKey = []byte{0x05}
)

/*
*
Transient Account State Keys
*/
var (
	AccountCreated = []byte{0x01}
	AccountDeleted = []byte{0x02}
)
