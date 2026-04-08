package vtype

const (
	// The number of bytes in the serialization version field of a vtype.
	VersionLength = 1
	// The number of bytes in the block height field of a vtype (for the types that have one).
	BlockHeightLength = 8
	// The length of a balance field (account)
	BalanceLength = 32
	// The length of a nonce field (account)
	NonceLength = 8
	// The length of a code hash field (account)
	CodeHashLength = 32
	// The length of a storage value field (storage)
	StorageValueLength = 32
)
