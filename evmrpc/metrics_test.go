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
}

func TestClassifyRPCMetricError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		panicked  bool
		wantClass string
		wantCode  int64
	}{
		{name: "ok", err: nil, panicked: false, wantClass: errorClassOK, wantCode: 0},
		{name: "panic", err: nil, panicked: true, wantClass: errorClassPanic, wantCode: internalErrorCode},
		{name: "panic_with_err", err: errors.New("ignored when panicked"), panicked: true, wantClass: errorClassPanic, wantCode: internalErrorCode},
		{name: "revert", err: NewRevertErrorFromError(errors.New("execution reverted")), wantClass: errorClassExecutionReverted, wantCode: 3},
		{name: "evm_not_supported", err: &ErrEVMNotSupported{Msg: "nope"}, wantClass: errorClassEVMNotSupported, wantCode: ErrCodeEVMNotSupported},
		{name: "sei_legacy", err: errSeiLegacyNotEnabledForTest("m"), wantClass: errorClassSeiLegacyDisabled, wantCode: seiLegacyNotEnabled},
		{name: "association", err: types.NewAssociationMissingErr("0xabc"), wantClass: errorClassAssociationMissing, wantCode: 0},
		{name: "wrapped_revert", err: errors.Join(errors.New("outer"), NewRevertErrorFromError(errors.New("execution reverted"))), wantClass: errorClassExecutionReverted, wantCode: 3},
		{name: "unknown", err: errors.New("something else"), wantClass: errorClassUnknown, wantCode: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotClass, gotCode := classifyRPCMetricError(tc.err, tc.panicked)
			if gotClass != tc.wantClass || gotCode != tc.wantCode {
				t.Fatalf("classifyRPCMetricError() = (%q, %d), want (%q, %d)", gotClass, gotCode, tc.wantClass, tc.wantCode)
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
