package types_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestCtxEvm(t *testing.T) {
	ctx := sdk.Context{}.WithContext(context.Background())
	require.Nil(t, types.GetCtxEVM(ctx))
	ctx = types.SetCtxEVM(ctx, &vm.EVM{})
	require.NotNil(t, types.GetCtxEVM(ctx))
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), types.CtxEVMKey, 123))
	require.Nil(t, types.GetCtxEVM(ctx))
}
