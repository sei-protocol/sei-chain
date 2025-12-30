package types

// The interface defines the entrypoints of a transaction execution.
type VM interface {
	Create(sender Address, code []byte, gas uint64, value Hash) (ret []byte, contractAddr Address, gasLeft uint64, err error)
	Call(sender Address, to Address, input []byte, gas uint64, value Hash) (ret []byte, gasLeft uint64, err error)
}

// The interface defines access to states. These are needed mainly
// for the preprocessing before the VM entrypoints are called (
// e.g. nonce checking/setting, fee charging, value transfer, etc.)
type Storage interface {
	GetCode(addr Address) ([]byte, error)
	GetState(addr Address, key Hash) (Hash, error)
	SetState(addr Address, key Hash, value Hash) error
	GetBalance(addr Address) (Hash, error)
	SetBalance(addr Address, value Hash) error
	GetNonce(addr Address) (uint64, error)
	SetNonce(addr Address, nonce uint64) error
	// TODO: accesslist setting
}
