package ethrpcerrors_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sei-protocol/sei-chain/evmrpc/ethrpcerrors"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

const (
	msgInternal      = "internal error"
	msgTimeout       = "request timed out"
	msgUnprotected   = "only replay-protected (EIP-155) transactions allowed over RPC"
	msgEmptyAuthList = "set code tx must have at least one authorization tuple"
	leakedDialURL    = "http://10.0.0.1:8545"
)

type goldenCase struct {
	name    string
	in      func() error
	code    int
	message string
}

func abci(codespace string, code uint32, log string) func() error {
	return func() error { return ethrpcerrors.TranslateABCI(codespace, code, log) }
}

func from(err error) func() error {
	return func() error { return ethrpcerrors.Translate(err) }
}

func fromText(msg string) func() error {
	return from(errors.New(msg))
}

func withDetail(sentinel error, detail string) string {
	if detail == "" {
		return sentinel.Error()
	}
	return sentinel.Error() + ": " + detail
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{
			name:    "abci wrong sequence",
			in:      abci("sdk", 32, "incorrect account sequence"),
			code:    -32000,
			message: core.ErrNonceTooLow.Error(),
		},
		{
			name:    "abci wrong sequence with next/tx nonce",
			in:      abci("sdk", 32, "next nonce 5, tx nonce 3: incorrect account sequence"),
			code:    -32000,
			message: withDetail(core.ErrNonceTooLow, "next nonce 5, tx nonce 3"),
		},
		{
			name:    "abci nonce too high detail",
			in:      abci("sdk", 32, "nonce too high: address 0xabc, tx: 7 state: 5: incorrect account sequence"),
			code:    -32000,
			message: withDetail(core.ErrNonceTooHigh, "address 0xabc, tx: 7 state: 5"),
		},
		{
			name:    "abci insufficient funds",
			in:      abci("sdk", 5, "insufficient funds"),
			code:    -32000,
			message: core.ErrInsufficientFunds.Error(),
		},
		{
			name:    "abci insufficient funds with address",
			in:      abci("sdk", 5, "insufficient funds for gas * price + value: address 0xabc have 0 want 21000: insufficient funds"),
			code:    -32000,
			message: withDetail(core.ErrInsufficientFunds, "address 0xabc have 0 want 21000"),
		},
		{
			name:    "abci insufficient fee",
			in:      abci("sdk", 13, "insufficient fee"),
			code:    -32000,
			message: core.ErrFeeCapTooLow.Error(),
		},
		{
			name:    "abci insufficient fee baseFee",
			in:      abci("sdk", 13, "address 0xabc, maxFeePerGas: 1, baseFee: 1000000000: insufficient fee"),
			code:    -32000,
			message: withDetail(core.ErrFeeCapTooLow, "address 0xabc, maxFeePerGas: 1, baseFee: 1000000000"),
		},
		{
			name:    "abci insufficient fee minimumFeePerGas",
			in:      abci("sdk", 13, "address 0xabc, maxFeePerGas: 1, minimumFeePerGas: 1000000000: insufficient fee"),
			code:    -32000,
			message: withDetail(core.ErrFeeCapTooLow, "address 0xabc, maxFeePerGas: 1, minimumFeePerGas: 1000000000"),
		},
		{
			name:    "abci unsupported tx type",
			in:      abci("sdk", 44, "unsupported transaction type"),
			code:    -32000,
			message: core.ErrTxTypeNotSupported.Error(),
		},
		{
			name:    "abci unsupported tx type with pool detail",
			in:      abci("sdk", 44, "tx type 4 not supported by this pool: unsupported transaction type"),
			code:    -32000,
			message: withDetail(core.ErrTxTypeNotSupported, "tx type 4 not supported by this pool"),
		},
		{
			name:    "abci invalid chain-id",
			in:      abci("sdk", 28, "invalid chain-id"),
			code:    -32000,
			message: withDetail(txpool.ErrInvalidSender, ethtypes.ErrInvalidChainId.Error()),
		},
		{
			name:    "abci invalid chain-id have/want",
			in:      abci("sdk", 28, "invalid chain id for signer: have 999999 want 1329: invalid chain-id"),
			code:    -32000,
			message: withDetail(txpool.ErrInvalidSender, ethtypes.ErrInvalidChainId.Error()+": have 999999 want 1329"),
		},
		{
			name:    "abci invalid v,r,s under chain-id",
			in:      abci("sdk", 28, "invalid transaction v, r, s values: invalid chain-id"),
			code:    -32000,
			message: withDetail(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error()),
		},
		{
			name:    "abci out of gas exceeds block limit",
			in:      abci("sdk", 11, "tx gas limit 20000000 exceeds block max gas 12500000: out of gas"),
			code:    -32000,
			message: withDetail(txpool.ErrGasLimit, "tx gas limit 20000000 exceeds block max gas 12500000"),
		},
		{
			name:    "abci invalid coins",
			in:      abci("sdk", 10, "invalid coins"),
			code:    -32000,
			message: txpool.ErrNegativeValue.Error(),
		},
		{
			name:    "abci tx already in mempool",
			in:      abci("sdk", 19, "tx already in mempool"),
			code:    -32000,
			message: txpool.ErrAlreadyKnown.Error(),
		},
		{
			name:    "abci mempool is full",
			in:      abci("sdk", 20, "mempool is full"),
			code:    -32000,
			message: legacypool.ErrTxPoolOverflow.Error(),
		},
		{
			name:    "abci tx too large",
			in:      abci("sdk", 21, "tx too large"),
			code:    -32000,
			message: txpool.ErrOversizedData.Error(),
		},
		{
			name:    "abci sdk code with no geth analogue",
			in:      abci("sdk", 18, "not EVM message: invalid request"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "abci sdk code 1 empty log",
			in:      abci("sdk", 1, ""),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "abci foreign codespace",
			in:      abci("test", 3, "log"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "abci undefined intrinsic gas with detail",
			in:      abci("undefined", 1, "intrinsic gas too low: gas 1000, minimum needed 21000"),
			code:    -32000,
			message: withDetail(core.ErrIntrinsicGas, "gas 1000, minimum needed 21000"),
		},
		{
			name:    "abci undefined intrinsic gas",
			in:      abci("undefined", 1, "intrinsic gas too low"),
			code:    -32000,
			message: core.ErrIntrinsicGas.Error(),
		},
		{
			name:    "abci undefined floor data gas",
			in:      abci("undefined", 1, "insufficient gas for floor data gas cost: gas 40000, minimum needed 61000"),
			code:    -32000,
			message: withDetail(core.ErrFloorDataGas, "gas 40000, minimum needed 61000"),
		},
		{
			name:    "abci undefined max initcode size",
			in:      abci("undefined", 1, "max initcode size exceeded: code size 49153, limit 49152"),
			code:    -32000,
			message: withDetail(core.ErrMaxInitCodeSizeExceeded, "code size 49153, limit 49152"),
		},
		{
			name:    "abci undefined unsafe legacy tx",
			in:      abci("undefined", 1, "unsupported tx type: unsafe legacy tx"),
			code:    -32000,
			message: msgUnprotected,
		},
		{
			name:    "text empty set-code authorization list",
			in:      fromText("auth list cannot be empty"),
			code:    -32000,
			message: msgEmptyAuthList,
		},
		{
			name:    "text oversized signature v",
			in:      fromText("invalid v: too long"),
			code:    -32000,
			message: withDetail(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error()),
		},
		{
			name:    "text oversized signature r",
			in:      fromText("invalid r: too long"),
			code:    -32000,
			message: withDetail(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error()),
		},
		{
			name:    "text oversized signature s",
			in:      fromText("invalid s: too long"),
			code:    -32000,
			message: withDetail(txpool.ErrInvalidSender, ethtypes.ErrInvalidSig.Error()),
		},
		{
			name:    "abci undefined tip above fee cap",
			in:      abci("undefined", 1, "max priority fee per gas higher than max fee per gas (400000000000 > 200000000000)"),
			code:    -32000,
			message: core.ErrTipAboveFeeCap.Error() + " (400000000000 > 200000000000)",
		},
		{
			name:    "abci undefined fee out of bound",
			in:      abci("undefined", 1, "fee out of bound"),
			code:    -32000,
			message: withDetail(core.ErrInsufficientFunds, "fee out of bound"),
		},
		{
			name:    "abci undefined value overflow",
			in:      abci("undefined", 1, "value overflow"),
			code:    -32000,
			message: withDetail(core.ErrInsufficientFunds, "value overflow"),
		},
		{
			name:    "abci undefined gas price overflow",
			in:      abci("undefined", 1, "gas price overflow"),
			code:    -32000,
			message: core.ErrFeeCapVeryHigh.Error(),
		},
		{
			name:    "abci undefined gas tip cap overflow",
			in:      abci("undefined", 1, "gas tip cap overflow"),
			code:    -32000,
			message: core.ErrTipVeryHigh.Error(),
		},
		{
			name:    "abci undefined tx gas exceeds max",
			in:      abci("undefined", 1, "tx gas exceeds max"),
			code:    -32000,
			message: txpool.ErrGasLimit.Error(),
		},
		{
			name:    "abci oracle unauthorized voter",
			in:      abci("oracle", 5, "unauthorized voter"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text tx already exists in cache",
			in:      fromText("tx already exists in cache"),
			code:    -32000,
			message: txpool.ErrAlreadyKnown.Error(),
		},
		{
			name:    "text duplicate tx",
			in:      fromText("duplicate tx"),
			code:    -32000,
			message: txpool.ErrAlreadyKnown.Error(),
		},
		{
			name:    "text tx with this nonce already in mempool",
			in:      fromText("tx with this nonce already in mempool"),
			code:    -32000,
			message: txpool.ErrReplaceUnderpriced.Error(),
		},
		{
			name:    "text tx too large classic",
			in:      fromText("tx too large: max size is 100, but got 200"),
			code:    -32000,
			message: withDetail(txpool.ErrOversizedData, "max size is 100, but got 200"),
		},
		{
			name:    "text tx too large alt",
			in:      fromText("tx too large: tx size is too big: 200, max: 100"),
			code:    -32000,
			message: withDetail(txpool.ErrOversizedData, "tx size is too big: 200, max: 100"),
		},
		{
			name:    "text nonce too old",
			in:      fromText("nonce too old"),
			code:    -32000,
			message: core.ErrNonceTooLow.Error(),
		},
		{
			name:    "text mempool full",
			in:      fromText("mempool full"),
			code:    -32000,
			message: legacypool.ErrTxPoolOverflow.Error(),
		},
		{
			name:    "text mempool is full",
			in:      fromText("mempool is full"),
			code:    -32000,
			message: legacypool.ErrTxPoolOverflow.Error(),
		},
		{
			name:    "text priority not high enough",
			in:      fromText("priority not high enough for mempool"),
			code:    -32000,
			message: txpool.ErrUnderpriced.Error(),
		},
		{
			name:    "text gas wanted exceeds max gas",
			in:      fromText("gas wanted exceeds max gas: gas wanted 30000000 is greater than max gas 12500000"),
			code:    -32000,
			message: withDetail(txpool.ErrGasLimit, "gas wanted 30000000 is greater than max gas 12500000"),
		},
		{
			name:    "text transaction too large",
			in:      fromText("transaction too large"),
			code:    -32000,
			message: txpool.ErrOversizedData.Error(),
		},
		{
			name:    "text bad nonce too low",
			in:      fromText("bad nonce: got 3, want 5"),
			code:    -32000,
			message: withDetail(core.ErrNonceTooLow, "next nonce 5, tx nonce 3"),
		},
		{
			name:    "text bad nonce too high",
			in:      fromText("bad nonce: got 9, want 5"),
			code:    -32000,
			message: withDetail(core.ErrNonceTooHigh, "tx nonce 9, gapped nonce 5"),
		},
		{
			name:    "text bad nonce garbage",
			in:      fromText("bad nonce: garbage"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text not producing",
			in:      fromText("not producing"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text autobahn fullnode no mempool",
			in:      fromText("autobahn fullnode has no local mempool; broadcast_tx_* must be sent to a validator"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text context canceled",
			in:      fromText("context canceled"),
			code:    -32002,
			message: msgTimeout,
		},
		{
			name:    "text context deadline exceeded",
			in:      fromText("context deadline exceeded"),
			code:    -32002,
			message: msgTimeout,
		},
		{
			name:    "text timeout waiting for commit",
			in:      fromText("timeout waiting for commit of tx 0xabc (1.5s)"),
			code:    -32002,
			message: msgTimeout,
		},
		{
			name:    "text panic recovered in CheckTxSafe",
			in:      fromText("panic recovered in CheckTxSafe"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text nil response",
			in:      fromText("nil response"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text EVM response missing EVMHash",
			in:      fromText("EVM response missing EVMHash"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text EVM response missing SeiSenderAddress",
			in:      fromText("EVM response missing SeiSenderAddress"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text missing broadcast response",
			in:      fromText("missing broadcast response"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text mempool is not available",
			in:      fromText("mempool is not available"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text kvEventSink not enabled",
			in:      fromText("cannot confirm transaction because kvEventSink is not enabled"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text broadcast_tx_commit unsupported",
			in:      fromText("broadcast_tx_commit is not supported on Autobahn; use broadcast_tx_sync"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text txConstraintsFetcher",
			in:      fromText("txmp.txConstraintsFetcher(): boom"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text negative gas wanted",
			in:      fromText("negative gas wanted: -5"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name: "url.Error connection refused",
			in: from(&url.Error{
				Op:  "Post",
				URL: leakedDialURL,
				Err: errors.New("dial tcp: connection refused"),
			}),
			code:    -32603,
			message: msgInternal,
		},
		{
			name: "rpc.HTTPError 502",
			in: from(&rpc.HTTPError{
				StatusCode: 502,
				Status:     "502 Bad Gateway",
				Body:       []byte("upstream"),
			}),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "text unknown mempoolish",
			in:      fromText("mempoolish"),
			code:    -32603,
			message: msgInternal,
		},
		{
			name:    "context.DeadlineExceeded",
			in:      from(context.DeadlineExceeded),
			code:    -32002,
			message: msgTimeout,
		},
		{
			name:    "wrapped context.Canceled",
			in:      from(fmt.Errorf("wait: %w", context.Canceled)),
			code:    -32002,
			message: msgTimeout,
		},
		{
			name:    "sdk ErrWrongSequence",
			in:      from(sdkerrors.ErrWrongSequence),
			code:    -32000,
			message: core.ErrNonceTooLow.Error(),
		},
		{
			name:    "sdk Wrapf ErrWrongSequence",
			in:      from(sdkerrors.Wrapf(sdkerrors.ErrWrongSequence, "next nonce %d, tx nonce %d", 5, 3)),
			code:    -32000,
			message: withDetail(core.ErrNonceTooLow, "next nonce 5, tx nonce 3"),
		},
		{
			name: "sdk Wrap ErrInsufficientFunds",
			in: from(sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds,
				"insufficient funds for gas * price + value: address 0xabc have 0 want 21000")),
			code:    -32000,
			message: withDetail(core.ErrInsufficientFunds, "address 0xabc have 0 want 21000"),
		},
		{
			name:    "wrapped core.ErrIntrinsicGas",
			in:      from(fmt.Errorf("%w: gas %v, minimum needed %v", core.ErrIntrinsicGas, 1000, 21000)),
			code:    -32000,
			message: withDetail(core.ErrIntrinsicGas, "gas 1000, minimum needed 21000"),
		},
	}
}

func assertTranslated(t *testing.T, err error, code int, message string) {
	t.Helper()
	require.NotNil(t, err)
	require.IsType(t, (*ethrpcerrors.Error)(nil), err)
	rpcErr, ok := err.(rpc.Error)
	require.True(t, ok)
	require.Equal(t, code, rpcErr.ErrorCode())
	require.Equal(t, message, err.Error())
	dataErr, ok := err.(rpc.DataError)
	require.True(t, ok)
	require.Nil(t, dataErr.ErrorData())
}

func TestGolden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in()
			assertTranslated(t, err, tc.code, tc.message)
			if tc.name == "url.Error connection refused" {
				require.NotContains(t, err.Error(), leakedDialURL)
			}
		})
	}
}

func TestUnwrapsGethSentinel(t *testing.T) {
	require.ErrorIs(t, ethrpcerrors.Translate(sdkerrors.ErrWrongSequence), core.ErrNonceTooLow)
	require.ErrorIs(t, ethrpcerrors.Translate(sdkerrors.ErrInsufficientFee), core.ErrFeeCapTooLow)
	require.ErrorIs(t, ethrpcerrors.Translate(errors.New("tx already exists in cache")), txpool.ErrAlreadyKnown)
	require.ErrorIs(t, ethrpcerrors.Translate(errors.New("mempool full")), legacypool.ErrTxPoolOverflow)
	require.Nil(t, errors.Unwrap(ethrpcerrors.Translate(errors.New("mempoolish"))))
}

type alreadyRPCError struct {
	code int
	msg  string
}

func (e *alreadyRPCError) Error() string  { return e.msg }
func (e *alreadyRPCError) ErrorCode() int { return e.code }

func TestPassThrough(t *testing.T) {
	require.Nil(t, ethrpcerrors.Translate(nil))
	require.Nil(t, ethrpcerrors.TranslateABCI("sdk", 0, ""))

	in := &alreadyRPCError{code: 3, msg: "already a json-rpc error"}
	require.Same(t, in, ethrpcerrors.Translate(in))

	httpErr := &rpc.HTTPError{StatusCode: 502, Status: "502 Bad Gateway", Body: []byte("upstream")}
	got := ethrpcerrors.Translate(httpErr)
	require.NotSame(t, httpErr, got)
	assertTranslated(t, got, -32603, msgInternal)
}

var cosmosVocabulary = []string{
	"incorrect account sequence",
	"insufficient fee",
	"invalid coins",
	"unknown request",
	"tx parse error",
	"codespace",
	"rpc error: code =",
	"panic recovered in CheckTxSafe",
	"bad nonce",
	"mempool is full",
	"mempool full",
	"not producing",
	"transaction too large",
	"tx too large",
	"nil response",
	"EVM response missing",
	"missing broadcast response",
	"broadcast_tx_commit is not supported",
	"transaction rejected with code",
	`Post "`,
	"unsupported transaction type",
	"invalid chain-id",
	"out of gas",
	"tx already exists in cache",
	"unsafe legacy tx",
	"auth list cannot be empty",
	"invalid v: too long",
	"invalid r: too long",
	"invalid s: too long",
	"unknown",
}

func TestNoCosmosVocabulary(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in()
			require.NotNil(t, err)
			msg := err.Error()
			require.NotEmpty(t, msg)
			require.False(t, strings.HasPrefix(msg, ": "))
			for _, banned := range cosmosVocabulary {
				require.NotContains(t, msg, banned)
			}
		})
	}
}

type roundTripService struct{}

func (*roundTripService) Translated(context.Context) (string, error) {
	return "", ethrpcerrors.TranslateABCI("sdk", 32, "next nonce 5, tx nonce 3: incorrect account sequence")
}

func (*roundTripService) Internal(context.Context) (string, error) {
	return "", ethrpcerrors.Translate(errors.New("mempoolish"))
}

func (*roundTripService) Wrapped(context.Context) (string, error) {
	return "", fmt.Errorf("outer: %w", ethrpcerrors.TranslateABCI("sdk", 32, "incorrect account sequence"))
}

func TestJSONRPCRoundTrip(t *testing.T) {
	srv := rpc.NewServer()
	t.Cleanup(srv.Stop)
	require.NoError(t, srv.RegisterName("test", &roundTripService{}))
	client := rpc.DialInProc(srv)
	t.Cleanup(client.Close)

	t.Run("translated", func(t *testing.T) {
		var res string
		err := client.CallContext(t.Context(), &res, "test_translated")
		rpcErr, ok := err.(rpc.Error)
		require.True(t, ok)
		require.Equal(t, -32000, rpcErr.ErrorCode())
		require.Equal(t, withDetail(core.ErrNonceTooLow, "next nonce 5, tx nonce 3"), err.Error())
		dataErr, ok := err.(rpc.DataError)
		require.True(t, ok)
		require.Nil(t, dataErr.ErrorData())
	})
	t.Run("internal", func(t *testing.T) {
		var res string
		err := client.CallContext(t.Context(), &res, "test_internal")
		rpcErr, ok := err.(rpc.Error)
		require.True(t, ok)
		require.Equal(t, -32603, rpcErr.ErrorCode())
		require.Equal(t, msgInternal, err.Error())
	})
	t.Run("wrapped", func(t *testing.T) {
		var res string
		err := client.CallContext(t.Context(), &res, "test_wrapped")
		rpcErr, ok := err.(rpc.Error)
		require.True(t, ok)
		// A wrapped *ethrpcerrors.Error loses its ErrorCode; go-ethereum then
		// encodes the default -32000. Translate must be the top-level return.
		require.Equal(t, -32000, rpcErr.ErrorCode())
		require.Equal(t, "outer: "+core.ErrNonceTooLow.Error(), err.Error())
	})
}

func untranslatedTotal(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "evmrpc_untranslated_error_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

func TestUntranslatedCounter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(t.Context())
		otel.SetMeterProvider(prev)
	})

	before := untranslatedTotal(t, reader)
	_ = ethrpcerrors.Translate(errors.New("unknown xyz"))
	_ = ethrpcerrors.Translate(errors.New("unknown xyz"))
	after := untranslatedTotal(t, reader)
	require.Equal(t, before+2, after)
}
