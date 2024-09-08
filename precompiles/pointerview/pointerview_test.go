package pointerview_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/stretchr/testify/require"
)

func TestPointerView(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	p, err := pointerview.NewPrecompile(k)
	require.Nil(t, err)

	_, pointer := testkeeper.MockAddressPair()
	k.SetERC20NativePointer(ctx, "test", pointer)
	k.SetERC20CW20Pointer(ctx, "test", pointer)
	k.SetERC721CW721Pointer(ctx, "test", pointer)

	// Test GetNativePointer
	testGetNativePointer(t, ctx, p, pointer)

	// Test GetCW20Pointer
	testGetCW20Pointer(t, ctx, p, pointer)

	// Test GetCW721Pointer
	testGetCW721Pointer(t, ctx, p, pointer)

	// Test GetNativePointee
	testGetNativePointee(t, ctx, p, pointer)

	// Test GetCW20Pointee
	testGetCW20Pointee(t, ctx, p, pointer)

	// Test GetCW721Pointee
	testGetCW721Pointee(t, ctx, p, pointer)
}

func testGetNativePointer(t *testing.T, ctx testkeeper.TestContext, p *pointerview.Precompile, pointer common.Address) {
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
}

func testGetCW20Pointer(t *testing.T, ctx testkeeper.TestContext, p *pointerview.Precompile, pointer common.Address) {
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20PointerID)
	require.Nil(t, err)

	ret, err := p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20(ctx, m, []interface{}{"test"})
	require.Nil(t, err)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, pointer, outputs[0].(common.Address))
	require.Equal(t, cw20.CurrentVersion(ctx), outputs[1].(uint16))
	require.True(t, outputs[2].(bool))

	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20(ctx, m, []interface{}{"test2"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))
}

func testGetCW721Pointer(t *testing.T, ctx testkeeper.TestContext, p *pointerview.Precompile, pointer common.Address) {
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721PointerID)
	require.Nil(t, err)

	ret, err := p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721(ctx, m, []interface{}{"test"})
	require.Nil(t, err)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, pointer, outputs[0].(common.Address))
	require.Equal(t, cw721.CurrentVersion, outputs[1].(uint16))
	require.True(t, outputs[2].(bool))

	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721(ctx, m, []interface{}{"test2"})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))
}

func testGetNativePointee(t *testing.T, ctx testkeeper.TestContext, p *pointerview.Precompile, pointer common.Address) {
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetNativePointeeID)
	require.Nil(t, err)

	ret, err := p.GetExecutor().(*pointerview.PrecompileExecutor).GetNativePointee(ctx, m, []interface{}{pointer})
	require.Nil(t, err)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, "test", outputs[0].(string))
	require.Equal(t, native.CurrentVersion, outputs[1].(uint16))
	require.True(t, outputs[2].(bool))

	invalidAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetNativePointee(ctx, m, []interface{}{invalidAddr})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))
}

func testGetCW20Pointee(t *testing.T, ctx testkeeper.TestContext, p *pointerview.Precompile, pointer common.Address) {
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20PointeeID)
	require.Nil(t, err)

	ret, err := p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20Pointee(ctx, m, []interface{}{pointer})
	require.Nil(t, err)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, "test", outputs[0].(string))
	require.Equal(t, cw20.CurrentVersion(ctx), outputs[1].(uint16))
	require.True(t, outputs[2].(bool))

	invalidAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW20Pointee(ctx, m, []interface{}{invalidAddr})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))
}

func testGetCW721Pointee(t *testing.T, ctx testkeeper.TestContext, p *pointerview.Precompile, pointer common.Address) {
	m, err := p.ABI.MethodById(p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721PointeeID)
	require.Nil(t, err)

	ret, err := p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721Pointee(ctx, m, []interface{}{pointer})
	require.Nil(t, err)
	outputs, err := m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Equal(t, "test", outputs[0].(string))
	require.Equal(t, cw721.CurrentVersion, outputs[1].(uint16))
	require.True(t, outputs[2].(bool))

	invalidAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ret, err = p.GetExecutor().(*pointerview.PrecompileExecutor).GetCW721Pointee(ctx, m, []interface{}{invalidAddr})
	require.Nil(t, err)
	outputs, err = m.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.False(t, outputs[2].(bool))
}