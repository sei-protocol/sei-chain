// Package ethrpcerrors maps EVM transaction submission errors onto the go-ethereum
// errors a JSON-RPC client expects.
package ethrpcerrors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// JSON-RPC error codes go-ethereum's rpc package assigns but does not export.
const (
	// CodeDefault is the code of every handler error that carries no code of its own.
	CodeDefault = -32000
	// CodeTimeout is the code go-ethereum's server writes when a call exceeds its deadline.
	CodeTimeout = -32002
	// CodeInternal is the JSON-RPC internal-error code used for unmapped server failures.
	CodeInternal = -32603
)

// Message literals go-ethereum defines inline rather than as exported sentinels.
const (
	msgInternal = "internal error"
	msgTimeout  = "request timed out"
	// msgUnprotected is ethapi.SubmitTransaction's rejection of a pre-EIP-155 transaction.
	msgUnprotected = "only replay-protected (EIP-155) transactions allowed over RPC"
	// msgEmptyAuthList is txpool.ValidateTransaction's rejection of an empty EIP-7702 authorization list.
	msgEmptyAuthList = "set code tx must have at least one authorization tuple"
)

var (
	logger = seilog.NewLogger("evmrpc", "ethrpcerrors")

	untranslatedCount = must(otel.Meter("evmrpc").Int64Counter(
		"evmrpc_untranslated_error_total",
		metric.WithDescription("Number of EVM submission errors with no go-ethereum mapping, returned as -32603 internal error"),
		metric.WithUnit("{count}"),
	))
)

// Error is a top-level JSON-RPC error carrying a go-ethereum message and code.
type Error struct {
	code    int
	message string
	// sentinel is the go-ethereum error the message is built on, or nil for internal errors.
	sentinel error
}

var (
	_ rpc.Error     = (*Error)(nil)
	_ rpc.DataError = (*Error)(nil)
)

func (e *Error) Error() string { return e.message }

// ErrorCode returns the JSON-RPC error code.
func (e *Error) ErrorCode() int { return e.code }

// ErrorData returns nil; go-ethereum attaches no data to this class of error.
func (e *Error) ErrorData() interface{} { return nil }

// Unwrap returns the go-ethereum sentinel the message is built on, so errors.Is can select on it.
func (e *Error) Unwrap() error { return e.sentinel }

// Translate returns the go-ethereum error a client expects in place of err. An error that
// already implements rpc.Error, such as a remote node's JSON-RPC error or a typed evmrpc
// error, is returned unchanged. A nil err returns nil.
func Translate(err error) error {
	if err == nil {
		return nil
	}
	// A direct assertion, not errors.As, mirrors what go-ethereum's server will do with the
	// value: only a top-level rpc.Error keeps its code, so only that is already at parity.
	if _, ok := err.(rpc.Error); ok {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return timeout()
	}
	codespace, code, log := sdkerrors.ABCIInfo(err, false)
	return TranslateABCI(codespace, code, log)
}

// TranslateABCI returns the go-ethereum error a client expects for a CheckTx rejection, or nil
// when code reports success.
func TranslateABCI(codespace string, code uint32, log string) error {
	if code == sdkerrors.SuccessABCICode {
		return nil
	}
	text := strings.TrimSpace(log)
	if codespace == sdkerrors.RootCodespace {
		if r, ok := sdkRuleFor(code); ok {
			return r.translate(text)
		}
	}
	return translateText(text)
}

// sdkRule maps a registered Cosmos SDK error onto the go-ethereum sentinel for the same condition.
type sdkRule struct {
	sdkErr   *sdkerrors.Error
	sentinel error
	// defaultDetail is the detail go-ethereum attaches when the producer supplied none.
	defaultDetail string
}

var sdkRules = []sdkRule{
	{sdkErr: sdkerrors.ErrWrongSequence, sentinel: core.ErrNonceTooLow},
	{sdkErr: sdkerrors.ErrInsufficientFunds, sentinel: core.ErrInsufficientFunds},
	{sdkErr: sdkerrors.ErrInsufficientFee, sentinel: core.ErrFeeCapTooLow},
	{sdkErr: sdkerrors.ErrUnsupportedTxType, sentinel: core.ErrTxTypeNotSupported},
	{sdkErr: sdkerrors.ErrInvalidChainID, sentinel: txpool.ErrInvalidSender, defaultDetail: ethtypes.ErrInvalidChainId.Error()},
	{sdkErr: sdkerrors.ErrOutOfGas, sentinel: txpool.ErrGasLimit},
	{sdkErr: sdkerrors.ErrInvalidCoins, sentinel: txpool.ErrNegativeValue},
	{sdkErr: sdkerrors.ErrTxInMempoolCache, sentinel: txpool.ErrAlreadyKnown},
	{sdkErr: sdkerrors.ErrMempoolIsFull, sentinel: legacypool.ErrTxPoolOverflow},
	{sdkErr: sdkerrors.ErrTxTooLarge, sentinel: txpool.ErrOversizedData},
}

func sdkRuleFor(code uint32) (sdkRule, bool) {
	for _, r := range sdkRules {
		if r.sdkErr.ABCICode() == code {
			return r, true
		}
	}
	return sdkRule{}, false
}

// translate builds the message from the detail the producer wrapped around the SDK error.
func (r sdkRule) translate(log string) *Error {
	detail := trimDescription(log, r.sdkErr.Error())
	// A producer that already speaks go-ethereum, such as an ante handler forwarding
	// go-ethereum's own StatelessChecks error, needs no sentinel prepended.
	if e, ok := matchGeth(detail); ok {
		return e
	}
	if detail == "" {
		detail = r.defaultDetail
	}
	return fromSentinel(r.sentinel, detail)
}

// trimDescription removes the registered description that sdkerrors.Wrap appends to an error's
// text, leaving the producer's own detail.
func trimDescription(log, description string) string {
	if log == description {
		return ""
	}
	return strings.TrimSuffix(log, ": "+description)
}

// gethSentinels are the go-ethereum errors a Sei producer may emit verbatim. A text that begins
// with one is already at parity and passes through under the default code.
var gethSentinels = []error{
	core.ErrNonceTooLow,
	core.ErrNonceTooHigh,
	core.ErrNonceMax,
	core.ErrIntrinsicGas,
	core.ErrFloorDataGas,
	core.ErrMaxInitCodeSizeExceeded,
	core.ErrInsufficientFunds,
	core.ErrInsufficientFundsForTransfer,
	core.ErrGasUintOverflow,
	core.ErrGasLimitReached,
	core.ErrTipAboveFeeCap,
	core.ErrTipVeryHigh,
	core.ErrFeeCapVeryHigh,
	core.ErrFeeCapTooLow,
	core.ErrBlobFeeCapTooLow,
	core.ErrSenderNoEOA,
	core.ErrTxTypeNotSupported,
	txpool.ErrAlreadyKnown,
	txpool.ErrInvalidSender,
	txpool.ErrUnderpriced,
	txpool.ErrReplaceUnderpriced,
	txpool.ErrAccountLimitExceeded,
	txpool.ErrGasLimit,
	txpool.ErrNegativeValue,
	txpool.ErrOversizedData,
	legacypool.ErrTxPoolOverflow,
}

func matchGeth(text string) (*Error, bool) {
	for _, s := range gethSentinels {
		if hasSentinelPrefix(text, s.Error()) {
			return &Error{code: CodeDefault, message: text, sentinel: s}, true
		}
	}
	if text == msgUnprotected {
		return &Error{code: CodeDefault, message: text}, true
	}
	return nil, false
}

// seiRule maps the text of a Sei-side error onto its go-ethereum equivalent. A message matches
// when it is prefix or continues it at a word boundary; the remainder after ": " is the detail.
type seiRule struct {
	prefix string
	to     func(detail string) *Error
}

var seiRules = []seiRule{
	// EVM ante and transaction-conversion errors that carry no ABCI code.
	{prefix: "unsupported tx type: unsafe legacy tx", to: literal(CodeDefault, msgUnprotected)},
	{prefix: "auth list cannot be empty", to: literal(CodeDefault, msgEmptyAuthList)},
	{prefix: "invalid v: too long", to: sentinelWithReason(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error())},
	{prefix: "invalid r: too long", to: sentinelWithReason(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error())},
	{prefix: "invalid s: too long", to: sentinelWithReason(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error())},
	{prefix: "tx gas exceeds max", to: sentinel(txpool.ErrGasLimit)},
	// x/evm/types/ethtx validation of 256-bit overflow. go-ethereum has no check for a fee or
	// value beyond 2^256-1; such a transaction fails its balance check instead.
	{prefix: "fee out of bound", to: sentinelWithReason(core.ErrInsufficientFunds, "fee out of bound")},
	{prefix: "value overflow", to: sentinelWithReason(core.ErrInsufficientFunds, "value overflow")},
	{prefix: "gas price overflow", to: sentinel(core.ErrFeeCapVeryHigh)},
	{prefix: "gas tip cap overflow", to: sentinel(core.ErrTipVeryHigh)},
	// sei-tendermint/internal/mempool, the classic mempool.
	{prefix: "tx already exists in cache", to: sentinel(txpool.ErrAlreadyKnown)},
	{prefix: "duplicate tx", to: sentinel(txpool.ErrAlreadyKnown)},
	{prefix: "tx with this nonce already in mempool", to: sentinel(txpool.ErrReplaceUnderpriced)},
	{prefix: "tx too large", to: sentinelWithDetail(txpool.ErrOversizedData)},
	{prefix: "nonce too old", to: sentinel(core.ErrNonceTooLow)},
	{prefix: "mempool full", to: sentinel(legacypool.ErrTxPoolOverflow)},
	{prefix: "priority not high enough for mempool", to: sentinel(txpool.ErrUnderpriced)},
	{prefix: "gas wanted exceeds max gas", to: sentinelWithDetail(txpool.ErrGasLimit)},
	{prefix: "txmp.txConstraintsFetcher()", to: internal},
	{prefix: "negative gas wanted", to: internal},
	// sei-tendermint/internal/autobahn/producer, the autobahn mempool.
	{prefix: "transaction too large", to: sentinel(txpool.ErrOversizedData)},
	{prefix: "mempool is full", to: sentinel(legacypool.ErrTxPoolOverflow)},
	{prefix: "bad nonce", to: badNonce},
	{prefix: "not producing", to: internal},
	{prefix: context.Canceled.Error(), to: literal(CodeTimeout, msgTimeout)},
	{prefix: context.DeadlineExceeded.Error(), to: literal(CodeTimeout, msgTimeout)},
	// sei-tendermint/internal/proxy.
	{prefix: "panic recovered in CheckTxSafe", to: internal},
	{prefix: "nil response", to: internal},
	{prefix: "EVM response missing", to: internal},
	// sei-tendermint/internal/rpc/core and evmrpc/send.go.
	{prefix: "autobahn fullnode has no local mempool", to: internal},
	{prefix: "mempool is not available", to: internal},
	{prefix: "cannot confirm transaction because kvEventSink is not enabled", to: internal},
	{prefix: "broadcast_tx_commit is not supported", to: internal},
	{prefix: "timeout waiting for commit of tx", to: literal(CodeTimeout, msgTimeout)},
	{prefix: "missing broadcast response", to: internal},
}

func translateText(text string) *Error {
	if e, ok := matchGeth(text); ok {
		return e
	}
	for _, r := range seiRules {
		if hasSentinelPrefix(text, r.prefix) {
			return r.to(strings.TrimPrefix(strings.TrimPrefix(text, r.prefix), ": "))
		}
	}
	return untranslated(text)
}

// hasSentinelPrefix reports whether text is sentinel or continues it at a word boundary, so that
// "nonce too low: next nonce 5, tx nonce 3" matches "nonce too low" and "mempool is full" does
// not match "mempool".
func hasSentinelPrefix(text, sentinel string) bool {
	if !strings.HasPrefix(text, sentinel) {
		return false
	}
	if len(text) == len(sentinel) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(text[len(sentinel):])
	return !unicode.IsLetter(next) && !unicode.IsDigit(next)
}

func fromSentinel(s error, detail string) *Error {
	message := s.Error()
	if detail != "" {
		message += ": " + detail
	}
	return &Error{code: CodeDefault, message: message, sentinel: s}
}

func sentinel(s error) func(string) *Error {
	return func(string) *Error { return fromSentinel(s, "") }
}

func sentinelWithDetail(s error) func(string) *Error {
	return func(detail string) *Error { return fromSentinel(s, detail) }
}

// sentinelWithReason maps a Sei-only rejection onto the nearest go-ethereum sentinel, keeping
// the Sei condition as the detail.
func sentinelWithReason(s error, reason string) func(string) *Error {
	return func(string) *Error { return fromSentinel(s, reason) }
}

func literal(code int, message string) func(string) *Error {
	return func(string) *Error { return &Error{code: code, message: message} }
}

func timeout() *Error {
	return &Error{code: CodeTimeout, message: msgTimeout}
}

// internal maps a Sei condition a go-ethereum node cannot be in onto its internal error.
func internal(detail string) *Error {
	logger.Debug("internal eth RPC error", "err", detail)
	return &Error{code: CodeInternal, message: msgInternal}
}

// badNonce maps the autobahn producer's strict-sequence rejection, "got N, want M", onto the
// txpool error for the direction of the mismatch.
func badNonce(detail string) *Error {
	var got, want uint64
	if _, err := fmt.Sscanf(detail, "got %d, want %d", &got, &want); err != nil {
		return untranslated("bad nonce: " + detail)
	}
	if got < want {
		return fromSentinel(core.ErrNonceTooLow, fmt.Sprintf("next nonce %d, tx nonce %d", want, got))
	}
	return fromSentinel(core.ErrNonceTooHigh, fmt.Sprintf("tx nonce %d, gapped nonce %d", got, want))
}

// untranslated is the fallback for an error the table does not know. The client sees an
// internal error; the original text is kept on the server side, where it is actionable.
func untranslated(text string) *Error {
	untranslatedCount.Add(context.Background(), 1)
	logger.Warn("untranslated eth RPC error", "err", text)
	return &Error{code: CodeInternal, message: msgInternal}
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
