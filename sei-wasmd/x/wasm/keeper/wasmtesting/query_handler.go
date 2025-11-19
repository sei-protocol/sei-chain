package wasmtesting

import (
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type MockQueryHandler struct {
	HandleQueryFn func(ctx sdk.Context, request wasmvmtypes.QueryRequest, caller seitypes.AccAddress) ([]byte, error)
}

func (m *MockQueryHandler) HandleQuery(ctx sdk.Context, caller seitypes.AccAddress, request wasmvmtypes.QueryRequest) ([]byte, error) {
	if m.HandleQueryFn == nil {
		panic("not expected to be called")
	}
	return m.HandleQueryFn(ctx, request, caller)
}
