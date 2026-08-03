package keeper_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
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
	res, err := k.QueryERCSingleOutput(ctx, "native", addr, "name")
	require.Nil(t, err)
	require.Equal(t, "test2", res.(string))
	res, err = k.QueryERCSingleOutput(ctx, "native", addr, "symbol")
	require.Nil(t, err)
	require.Equal(t, "test2", res.(string))
	res, err = k.QueryERCSingleOutput(ctx, "native", addr, "decimals")
	require.Nil(t, err)
	require.Equal(t, uint8(12), res.(uint8))
	_, err = k.QueryERCSingleOutput(ctx, "native", addr, "nonexist")
	require.NotNil(t, err)
}

// TestUpsertERCNativePointerKeepsCodeCacheCoherent covers the mid-tx redeploy
// path: plant a warm memo, re-upsert (exists → StateDB.SetCode with real
// deployment bytecode), and assert GetCode follows the store.
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
		return nil
	}, func(string, string) {})
	require.NoError(t, err)
	require.Equal(t, stalePlant, warmed)
	require.NotEqual(t, warmed, codeAfter)
	require.Equal(t, len(codeAfter), sizeAfter)
	require.Equal(t, codeAfter, storeCode)
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
