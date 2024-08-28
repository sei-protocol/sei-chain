package types

import (
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Codes for wasm contract errors
var (
	DefaultCodespace = ModuleName

	// Note: never use code 1 for any errors - that is reserved for ErrInternal in the core cosmos sdk

	// ErrCreateFailed error for wasm code that has already been uploaded or failed
	ErrCreateFailed = sdkErrors.Register(DefaultCodespace, 2, "create wasm contract failed")

	// ErrAccountExists error for a contract account that already exists
	ErrAccountExists = sdkErrors.Register(DefaultCodespace, 3, "contract account already exists")

	// ErrInstantiateFailed error for rust instantiate contract failure
	ErrInstantiateFailed = sdkErrors.Register(DefaultCodespace, 4, "instantiate wasm contract failed")

	// ErrExecuteFailed error for rust execution contract failure
	ErrExecuteFailed = sdkErrors.Register(DefaultCodespace, 5, "execute wasm contract failed")

	// ErrGasLimit error for out of gas
	ErrGasLimit = sdkErrors.Register(DefaultCodespace, 6, "insufficient gas")

	// ErrInvalidGenesis error for invalid genesis file syntax
	ErrInvalidGenesis = sdkErrors.Register(DefaultCodespace, 7, "invalid genesis")

	// ErrNotFound error for an entry not found in the store
	ErrNotFound = sdkErrors.Register(DefaultCodespace, 8, "not found")

	// ErrQueryFailed error for rust smart query contract failure
	ErrQueryFailed = sdkErrors.Register(DefaultCodespace, 9, "query wasm contract failed")

	// ErrInvalidMsg error when we cannot process the error returned from the contract
	ErrInvalidMsg = sdkErrors.Register(DefaultCodespace, 10, "invalid CosmosMsg from the contract")

	// ErrMigrationFailed error for rust execution contract failure
	ErrMigrationFailed = sdkErrors.Register(DefaultCodespace, 11, "migrate wasm contract failed")

	// ErrEmpty error for empty content
	ErrEmpty = sdkErrors.Register(DefaultCodespace, 12, "empty")

	// ErrLimit error for content that exceeds a limit
	ErrLimit = sdkErrors.Register(DefaultCodespace, 13, "exceeds limit")

	// ErrInvalid error for content that is invalid in this context
	ErrInvalid = sdkErrors.Register(DefaultCodespace, 14, "invalid")

	// ErrDuplicate error for content that exists
	ErrDuplicate = sdkErrors.Register(DefaultCodespace, 15, "duplicate")

	// ErrMaxIBCChannels error for maximum number of ibc channels reached
	ErrMaxIBCChannels = sdkErrors.Register(DefaultCodespace, 16, "max transfer channels")

	// ErrUnsupportedForContract error when a feature is used that is not supported for/ by this contract
	ErrUnsupportedForContract = sdkErrors.Register(DefaultCodespace, 17, "unsupported for this contract")

	// ErrPinContractFailed error for pinning contract failures
	ErrPinContractFailed = sdkErrors.Register(DefaultCodespace, 18, "pinning contract failed")

	// ErrUnpinContractFailed error for unpinning contract failures
	ErrUnpinContractFailed = sdkErrors.Register(DefaultCodespace, 19, "unpinning contract failed")

	// ErrUnknownMsg error by a message handler to show that it is not responsible for this message type
	ErrUnknownMsg = sdkErrors.Register(DefaultCodespace, 20, "unknown message from the contract")

	// ErrInvalidEvent error if an attribute/event from the contract is invalid
	ErrInvalidEvent = sdkErrors.Register(DefaultCodespace, 21, "invalid event")

	//  error if an address does not belong to a contract (just for registration)
	_ = sdkErrors.Register(DefaultCodespace, 22, "no such contract")

	// ErrNotAJSONObject error if given data is not a JSON object
	ErrNotAJSONObject = sdkErrors.Register(DefaultCodespace, 23, "not a JSON object")

	// ErrNoTopLevelKey error if a JSON object has no top-level key
	ErrNoTopLevelKey = sdkErrors.Register(DefaultCodespace, 24, "no top-level key")

	// ErrMultipleTopLevelKeys error if a JSON object has more than one top-level key
	ErrMultipleTopLevelKeys = sdkErrors.Register(DefaultCodespace, 25, "multiple top-level keys")

	// ErrTopKevelKeyNotAllowed error if a JSON object has a top-level key that is not allowed
	ErrTopKevelKeyNotAllowed = sdkErrors.Register(DefaultCodespace, 26, "top-level key is not allowed")

	// ErrExceedMaxQueryStackSize error if max query stack size is exceeded
	ErrExceedMaxQueryStackSize = sdkErrors.Register(DefaultCodespace, 27, "max query stack size exceeded")

	// unused 28..29

	// ErrExceedMaxCallDepth error if max query stack size is exceeded
	ErrExceedMaxCallDepth = sdkErrors.Register(DefaultCodespace, 30, "max call depth exceeded")
)

type ErrNoSuchContract struct {
	Addr string
}

func (m *ErrNoSuchContract) Error() string {
	return "no such contract: " + m.Addr
}

func (m *ErrNoSuchContract) ABCICode() uint32 {
	return 22
}

func (m *ErrNoSuchContract) Codespace() string {
	return DefaultCodespace
}
