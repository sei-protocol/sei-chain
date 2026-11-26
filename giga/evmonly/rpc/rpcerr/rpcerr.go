// Package rpcerr defines the JSON-RPC error codes the EVM-only RPC answers with, and Error, the
// error that carries one to the client.
package rpcerr

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Error codes defined by JSON-RPC 2.0.
const (
	// CodeParseError reports a request that is not valid JSON.
	CodeParseError = -32700
	// CodeInvalidRequest reports a request that is not a valid JSON-RPC request.
	CodeInvalidRequest = -32600
	// CodeMethodNotFound reports a method the server does not serve.
	CodeMethodNotFound = -32601
	// CodeInvalidParams reports parameters the method cannot accept.
	CodeInvalidParams = -32602
	// CodeInternalError reports a failure inside the server.
	CodeInternalError = -32603
)

// Error codes defined by EIP-1474, and code 3 for a reverted EVM call.
const (
	// CodeServerError is the code go-ethereum's server answers an error carrying no code of its own
	// with. EIP-1474 names it invalid input.
	CodeServerError = -32000
	// CodeResourceNotFound reports a requested resource that does not exist.
	CodeResourceNotFound = -32001
	// CodeResourceUnavailable reports a requested resource that exists but cannot be served now.
	CodeResourceUnavailable = -32002
	// CodeTransactionRejected reports a transaction the node refused to accept.
	CodeTransactionRejected = -32003
	// CodeMethodNotSupported reports a method the node does not support.
	CodeMethodNotSupported = -32004
	// CodeLimitExceeded reports a request exceeding a limit of the node; it may succeed when retried.
	CodeLimitExceeded = -32005
	// CodeExecutionReverted reports an EVM call that reverted, with the revert data as error data.
	CodeExecutionReverted = 3
)

// Error codes go-ethereum's server answers with on its own. Each shares its value with a code above.
const (
	// CodeRequestTimeout reports a method that outlived its deadline.
	CodeRequestTimeout = -32002
	// CodeResponseTooLarge reports a batch element refused because the batch response exceeded
	// its size limit.
	CodeResponseTooLarge = -32003
)

// MsgServerBusy is the message, or the end of the message, of a request refused because the node
// is at a concurrency limit.
const MsgServerBusy = "server busy"

// msgExecutionReverted is the message of a reverted call whose revert reason does not decode.
const msgExecutionReverted = "execution reverted"

// Error is a JSON-RPC error: the wrapped error's message, a code, and optional data. go-ethereum's
// server reads the code and data from the error a method returns, never from an error it wraps, so an
// Error must reach the server unwrapped.
type Error struct {
	code int
	err  error
	data any
}

// Error returns the message the client receives.
func (e *Error) Error() string {
	return e.err.Error()
}

// ErrorCode returns the JSON-RPC error code the client receives.
func (e *Error) ErrorCode() int {
	return e.code
}

// ErrorData returns the error data the client receives, or nil when there is none.
func (e *Error) ErrorData() any {
	return e.data
}

// Unwrap returns the error e reports.
func (e *Error) Unwrap() error {
	return e.err
}

// ServerError returns err as an error with CodeServerError.
func ServerError(err error) *Error {
	return &Error{code: CodeServerError, err: err}
}

// InvalidParams returns err as an error with CodeInvalidParams.
func InvalidParams(err error) *Error {
	return &Error{code: CodeInvalidParams, err: err}
}

// ResourceNotFound returns err as an error with CodeResourceNotFound.
func ResourceNotFound(err error) *Error {
	return &Error{code: CodeResourceNotFound, err: err}
}

// ResourceUnavailable returns err as an error with CodeResourceUnavailable.
func ResourceUnavailable(err error) *Error {
	return &Error{code: CodeResourceUnavailable, err: err}
}

// TransactionRejected returns err as an error with CodeTransactionRejected.
func TransactionRejected(err error) *Error {
	return &Error{code: CodeTransactionRejected, err: err}
}

// LimitExceeded returns err as an error with CodeLimitExceeded.
func LimitExceeded(err error) *Error {
	return &Error{code: CodeLimitExceeded, err: err}
}

// ExecutionReverted returns the error for an EVM call that reverted with revert: CodeExecutionReverted,
// the hex-encoded revert as data, and the revert reason in the message when it decodes.
func ExecutionReverted(revert []byte) *Error {
	err := errors.New(msgExecutionReverted)
	if reason, unpackErr := abi.UnpackRevert(revert); unpackErr == nil {
		err = fmt.Errorf("%s: %v", msgExecutionReverted, reason)
	}
	return &Error{code: CodeExecutionReverted, err: err, data: hexutil.Encode(revert)}
}
