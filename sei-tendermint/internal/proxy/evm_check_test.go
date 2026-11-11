package proxy

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type evmCheckApp struct {
	EVMApplication
	check func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2
}

func (a *evmCheckApp) CheckTx(ctx context.Context, req *RequestCheckTxV2) *ResponseCheckTxV2 {
	return a.check(ctx, req)
}

func TestEVMCheckTxSafe(t *testing.T) {
	for _, tc := range []struct {
		name      string
		check     func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2
		wantError bool
	}{
		{name: "panic", wantError: true, check: func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2 { panic("failed check") }},
		{name: "nil response", wantError: true, check: func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2 { return nil }},
		{name: "missing hash", wantError: true, check: func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2 {
			return &ResponseCheckTxV2{ResponseCheckTx: &ResponseCheckTx{}, IsEVM: true, SeiSenderAddress: []byte{1}}
		}},
		{name: "missing sender", wantError: true, check: func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2 {
			return &ResponseCheckTxV2{ResponseCheckTx: &ResponseCheckTx{}, IsEVM: true, EVMHash: common.Hash{1}}
		}},
		{name: "valid", check: func(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2 {
			return &ResponseCheckTxV2{ResponseCheckTx: &ResponseCheckTx{}, IsEVM: true, EVMHash: common.Hash{1}, SeiSenderAddress: []byte{1}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := NewEVM(&evmCheckApp{EVMApplication: NewTestEVMApplication(abci.BaseApplication{}), check: tc.check})
			res, err := app.CheckTxSafe(t.Context(), &RequestCheckTxV2{Tx: []byte{1}})
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, res.IsOK())
		})
	}
}
