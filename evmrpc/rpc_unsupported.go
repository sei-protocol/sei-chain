package evmrpc

// ErrCodeEVMNotSupported is the JSON-RPC error code (-32000) for EVM RPC methods that
// Sei exposes explicitly but does not implement (clear client feedback vs -32601 method missing).
const ErrCodeEVMNotSupported = -32000

// ErrCodeBlobsNotSupported is the historical name for ErrCodeEVMNotSupported (eth_blobBaseFee).
const ErrCodeBlobsNotSupported = ErrCodeEVMNotSupported

// ErrEVMNotSupported is returned for such methods; the JSON-RPC layer maps it to code ErrCodeEVMNotSupported.
type ErrEVMNotSupported struct {
	Msg string
}

func (e *ErrEVMNotSupported) Error() string {
	return e.Msg
}

func (e *ErrEVMNotSupported) ErrorCode() int {
	return ErrCodeEVMNotSupported
}
