package types

const (
	ModuleName   = "nitro"
	StoreKey     = ModuleName
	RouterKey    = ModuleName
	QuerierRoute = ModuleName
	MemStoreKey  = "mem_nitro"
)

var (
	TransactionDataKey = "transactiondata"
	StateRootKey       = "stateroot"
	SenderKey          = "sender"
)

func GetTransactionDataKey() []byte {
	return []byte(TransactionDataKey)
}

func GetStateRootKey() []byte {
	return []byte(StateRootKey)
}

func GetSenderKey() []byte {
	return []byte(SenderKey)
}
