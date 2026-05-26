package evmrpc

import (
	"errors"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestRecordRPCMetricsNoPanic(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	endpoint := "eth_smoke_" + t.Name()
	recordRPCLatency(ctx, endpoint, "http", true, nil, false, time.Now().Add(-2*time.Millisecond))
	recordWebsocketConnect(ctx)
	recordRedirectedRequest(ctx, endpoint, "http")
}

func TestClassifyRPCMetricError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		panicked  bool
		wantClass string
		wantCode  string
	}{
		{name: "ok", err: nil, panicked: false, wantClass: "", wantCode: ""},
		{name: "panic", err: nil, panicked: true, wantClass: errorClassPanic, wantCode: jsonrpcCodeBucketSpec},
		{name: "panic_with_err", err: errors.New("ignored when panicked"), panicked: true, wantClass: errorClassPanic, wantCode: jsonrpcCodeBucketSpec},
		{name: "revert", err: NewRevertErrorFromError(errors.New("execution reverted")), wantClass: errorClassExecutionReverted, wantCode: jsonrpcCodeBucketOther},
		{name: "evm_not_supported", err: &ErrEVMNotSupported{Msg: "nope"}, wantClass: errorClassEVMNotSupported, wantCode: jsonrpcCodeBucketServer},
		{name: "sei_legacy", err: errSeiLegacyNotEnabledForTest("m"), wantClass: errorClassSeiLegacyDisabled, wantCode: jsonrpcCodeBucketSpec},
		{name: "association", err: types.NewAssociationMissingErr("0xabc"), wantClass: errorClassAssociationMissing, wantCode: ""},
		{name: "wrapped_revert", err: errors.Join(errors.New("outer"), NewRevertErrorFromError(errors.New("execution reverted"))), wantClass: errorClassExecutionReverted, wantCode: jsonrpcCodeBucketOther},
		{name: "unknown", err: errors.New("something else"), wantClass: errorClassUnknown, wantCode: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotClass, gotCode := classifyRPCMetricError(tc.err, tc.panicked)
			if gotClass != tc.wantClass || gotCode != tc.wantCode {
				t.Fatalf("classifyRPCMetricError() = (%q, %q), want (%q, %q)", gotClass, gotCode, tc.wantClass, tc.wantCode)
			}
		})
	}
}

// NewRevertErrorFromError builds a *RevertError for tests (minimal valid instance).
func NewRevertErrorFromError(err error) *RevertError {
	return &RevertError{error: err, reason: "0x"}
}

func errSeiLegacyNotEnabledForTest(method string) error {
	return &errSeiLegacyNotEnabled{method: method}
}
