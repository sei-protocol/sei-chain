package keeper_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/vm"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
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
