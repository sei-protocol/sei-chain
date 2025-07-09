package pointerview_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/stretchr/testify/require"
)

func TestPointerView(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	p, err := pointerview.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.Nil(t, err)
	_, pointer := testkeeper.MockAddressPair()
	k.SetERC20NativePointer(ctx, "test", pointer)
	k.SetERC20CW20Pointer(ctx, "test", pointer)
	k.SetERC721CW721Pointer(ctx, "test", pointer)
	k.SetERC1155CW1155Pointer(ctx, "test", pointer)
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetNativePointerID)
	require.Nil(t, err)
	ret, err := p.GetExecutor().(*pointerview.PrecompileExecutor).GetNative(ctx, m, []interface{}{"test"})
	require.Nil(t, err)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, pointer, outputs[0].(common.Address))
	require.Equal(t, native.CurrentVersion, outputs[1].(uint16))
	require.True(t, outputs[2].(bool))
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetNative(ctx, m, []interface{}{"test2"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))

	m, err = p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20PointerID)
	require.Nil(t, err)
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20(ctx, m, []interface{}{"test"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, pointer, outputs[0].(common.Address))
	require.Equal(t, cw20.CurrentVersion(ctx), outputs[1].(uint16))
	require.True(t, outputs[2].(bool))
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20(ctx, m, []interface{}{"test2"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))

	m, err = p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721PointerID)
	require.Nil(t, err)
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721(ctx, m, []interface{}{"test"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, pointer, outputs[0].(common.Address))
	require.Equal(t, cw721.CurrentVersion, outputs[1].(uint16))
	require.True(t, outputs[2].(bool))
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721(ctx, m, []interface{}{"test2"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))

	m, err = p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW1155PointerID)
	require.Nil(t, err)
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW1155(ctx, m, []interface{}{"test"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, pointer, outputs[0].(common.Address))
	require.Equal(t, cw1155.CurrentVersion, outputs[1].(uint16))
	require.True(t, outputs[2].(bool))
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW1155(ctx, m, []interface{}{"test2"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))
}
