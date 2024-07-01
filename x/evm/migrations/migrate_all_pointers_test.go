package migrations_test

import (
	"math"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigrateERCNativePointers(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	var pointerAddr common.Address
	require.Nil(t, k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, _, err := k.UpsertERCNativePointer(ctx, e, math.MaxUint64, "test", utils.ERCMetadata{Name: "name", Symbol: "symbol", Decimals: 6})
		pointerAddr = a
		return err
	}, func(s1, s2 string) {}))
	require.Nil(t, migrations.MigrateERCNativePointers(ctx, &k))
	// address should stay the same
	addr, _, _ := k.GetERC20NativePointer(ctx, "test")
	require.Equal(t, pointerAddr, addr)
}

func TestMigrateERCCW20Pointers(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	var pointerAddr common.Address
	require.Nil(t, k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, _, err := k.UpsertERCCW20Pointer(ctx, e, math.MaxUint64, "test", utils.ERCMetadata{Name: "name", Symbol: "symbol"})
		pointerAddr = a
		return err
	}, func(s1, s2 string) {}))
	require.Nil(t, migrations.MigrateERCCW20Pointers(ctx, &k))
	// address should stay the same
	addr, _, _ := k.GetERC20CW20Pointer(ctx, "test")
	require.Equal(t, pointerAddr, addr)
}

func TestMigrateERCCW721Pointers(t *testing.T) {
	k := testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	var pointerAddr common.Address
	require.Nil(t, k.RunWithOneOffEVMInstance(ctx, func(e *vm.EVM) error {
		a, _, err := k.UpsertERCCW721Pointer(ctx, e, math.MaxUint64, "test", utils.ERCMetadata{Name: "name", Symbol: "symbol"})
		pointerAddr = a
		return err
	}, func(s1, s2 string) {}))
	require.Nil(t, migrations.MigrateERCCW721Pointers(ctx, &k))
	// address should stay the same
	addr, _, _ := k.GetERC721CW721Pointer(ctx, "test")
	require.Equal(t, pointerAddr, addr)
}
