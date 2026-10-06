package keeper_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestRunWithOneOffEVMInstance(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	errLog := ""
	errRunner := func(*vm.EVM) error { return errors.New("test") }
	errLogger := func(a string, b string) { errLog = a + " " + b }
	require.NotNil(t, k.RunWithOneOffEVMInstance(ctx, errRunner, errLogger))
	require.Equal(t, "upserting pointer test", errLog)
	succLog := ""
	succRunner := func(*vm.EVM) error { return nil }
	succLogger := func(string, string) { succLog = "unexpected" }
	require.Nil(t, k.RunWithOneOffEVMInstance(ctx, succRunner, succLogger))
	require.Empty(t, succLog)
}

func TestUpsertERCNativePointer(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	var addr common.Address
	err := k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCNativePointer(ctx, e, "test", utils.ERCMetadata{
			Name:     "test",
			Symbol:   "test",
			Decimals: 6,
		})
		addr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	var newAddr common.Address
	err = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCNativePointer(ctx, e, "test", utils.ERCMetadata{
			Name:     "test2",
			Symbol:   "test2",
			Decimals: 12,
		})
		newAddr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	require.Equal(t, addr, newAddr)
	require.Equal(t, "test2", readNativePointer(t, ctx, k, addr, "name"))
	require.Equal(t, "test2", readNativePointer(t, ctx, k, addr, "symbol"))
	require.Equal(t, uint8(12), readNativePointer(t, ctx, k, addr, "decimals"))
}

// readNativePointer statically calls a single-output method on a deployed native pointer.
func readNativePointer(t *testing.T, ctx sdk.Context, k *keeper.Keeper, addr common.Address, method string) interface{} {
	t.Helper()
	payload, err := native.GetParsedABI().Pack(method)
	require.Nil(t, err)
	res, err := k.StaticCallEVM(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), &addr, payload)
	require.Nil(t, err)
	outputs, err := native.GetParsedABI().Unpack(method, res)
	require.Nil(t, err)
	require.Len(t, outputs, 1)
	return outputs[0]
}

// TestUpsertERCNativePointerKeepsCodeCacheCoherent covers the mid-tx redeploy
// path: plant a warm memo, re-upsert (exists → keeper SetCode + RefreshCodeCache),
// and assert GetCode follows the store.
func TestUpsertERCNativePointerKeepsCodeCacheCoherent(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	stalePlant := []byte{1, 2, 3, 4, 5}
	var warmed, codeAfter, storeCode []byte
	var sizeAfter int
	err := k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		addr, err := k.UpsertERCNativePointer(ctx, e, "cache-coherent", utils.ERCMetadata{
			Name:     "before",
			Symbol:   "before",
			Decimals: 6,
		})
		if err != nil {
			return err
		}
		sdb := state.GetDBImpl(e.StateDB)
		if sdb == nil {
			return errors.New("expected DBImpl StateDB")
		}
		// Distinct warm entry so a missed memo update is observable (metadata-only
		// redeploys often produce identical runtime bytecode).
		sdb.SetCode(addr, stalePlant)
		warmed = sdb.GetCode(addr)

		_, err = k.UpsertERCNativePointer(ctx, e, "cache-coherent", utils.ERCMetadata{
			Name:     "after",
			Symbol:   "after",
			Decimals: 8,
		})
		if err != nil {
			return err
		}
		codeAfter = sdb.GetCode(addr)
		sizeAfter = sdb.GetCodeSize(addr)
		storeCode = k.GetCode(sdb.Ctx(), addr)
		// Registry must be written on the live Multistore layer (sdb.Ctx), not the
		// Prepare-time ctx that GetDeploymentCode may have frozen.
		got, _, found := k.GetERC20NativePointer(sdb.Ctx(), "cache-coherent")
		if !found || got != addr {
			return fmt.Errorf("pointer registry not visible on live StateDB ctx: found=%v got=%s want=%s", found, got.Hex(), addr.Hex())
		}
		return nil
	}, func(string, string) {})
	require.NoError(t, err)
	require.Equal(t, stalePlant, warmed)
	require.NotEqual(t, warmed, codeAfter)
	require.Equal(t, len(codeAfter), sizeAfter)
	require.Equal(t, codeAfter, storeCode)
}

// TestUpsertERCNativePointerFailedRedeployPreservesCode ensures a failed
// GetDeploymentCode path does not SetCode (or RefreshCodeCache) over live
// pointer bytecode — even briefly — before returning the error.
func TestUpsertERCNativePointerFailedRedeployPreservesCode(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	preserved := []byte{9, 8, 7, 6, 5}
	var codeAfterFail, storeAfterFail []byte
	var sizeAfterFail int
	err := k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		addr, err := k.UpsertERCNativePointer(ctx, e, "fail-preserves", utils.ERCMetadata{
			Name:     "before",
			Symbol:   "before",
			Decimals: 6,
		})
		if err != nil {
			return err
		}
		sdb := state.GetDBImpl(e.StateDB)
		if sdb == nil {
			return errors.New("expected DBImpl StateDB")
		}
		sdb.SetCode(addr, preserved)
		require.Equal(t, preserved, sdb.GetCode(addr))

		// Finite cosmos meter large enough for the pointer-registry getter reads, but
		// small enough that GetDeploymentCode OOGs (normalizer is 1 in tests). StateDB
		// KV metering stays on the infinite RunWithOneOff meter.
		lowGasCtx := ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, 50_000))
		_, err = k.UpsertERCNativePointer(lowGasCtx, e, "fail-preserves", utils.ERCMetadata{
			Name:     "after",
			Symbol:   "after",
			Decimals: 8,
		})
		require.Error(t, err)

		codeAfterFail = sdb.GetCode(addr)
		sizeAfterFail = sdb.GetCodeSize(addr)
		storeAfterFail = k.GetCode(sdb.Ctx(), addr)
		return nil
	}, func(string, string) {})
	require.NoError(t, err)
	require.Equal(t, preserved, codeAfterFail)
	require.Equal(t, len(preserved), sizeAfterFail)
	require.Equal(t, preserved, storeAfterFail)
}

func TestUpsertERC20Pointer(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	var addr common.Address
	err := k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCCW20Pointer(ctx, e, "test", utils.ERCMetadata{
			Name:   "test",
			Symbol: "test",
		})
		addr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	var newAddr common.Address
	err = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCCW20Pointer(ctx, e, "test", utils.ERCMetadata{
			Name:   "test2",
			Symbol: "test2",
		})
		newAddr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	require.Equal(t, addr, newAddr)
}

func TestUpsertERC721Pointer(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	var addr common.Address
	err := k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCCW721Pointer(ctx, e, "test", utils.ERCMetadata{
			Name:   "test",
			Symbol: "test",
		})
		addr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	var newAddr common.Address
	err = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCCW721Pointer(ctx, e, "test", utils.ERCMetadata{
			Name:   "test2",
			Symbol: "test2",
		})
		newAddr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	require.Equal(t, addr, newAddr)
}

func TestUpsertERC1155Pointer(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	var addr common.Address
	err := k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCCW1155Pointer(ctx, e, "test", utils.ERCMetadata{
			Name:   "test",
			Symbol: "test",
		})
		addr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	var newAddr common.Address
	err = k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, err := k.UpsertERCCW1155Pointer(ctx, e, "test", utils.ERCMetadata{
			Name:   "test2",
			Symbol: "test2",
		})
		newAddr = a
		return err
	}, func(s1, s2 string) {})
	require.Nil(t, err)
	require.Equal(t, addr, newAddr)
}
