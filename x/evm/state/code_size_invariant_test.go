package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

// TestStateDBGetCodeSizeMatchesGetCodeLength pins the same size/code invariant
// through the EVM StateDB bridge (including snapshot/revert and account recreate).
func TestStateDBGetCodeSizeMatchesGetCodeLength(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)

	assertInvariant := func(t *testing.T) {
		t.Helper()
		code := statedb.GetCode(addr)
		require.Equal(t, len(code), statedb.GetCodeSize(addr), "GetCodeSize must equal len(GetCode)")
	}

	t.Run("empty", func(t *testing.T) {
		assertInvariant(t)
	})

	t.Run("set and resize", func(t *testing.T) {
		statedb.SetCode(addr, []byte{1, 2, 3})
		assertInvariant(t)

		designator := ethtypes.AddressToDelegation(common.BytesToAddress([]byte("target")))
		statedb.SetCode(addr, designator)
		assertInvariant(t)
		require.Equal(t, 23, statedb.GetCodeSize(addr))

		large := make([]byte, params.MaxCodeSize)
		statedb.SetCode(addr, large)
		assertInvariant(t)

		statedb.SetCode(addr, nil)
		assertInvariant(t)
		require.Equal(t, 0, statedb.GetCodeSize(addr))
	})

	t.Run("snapshot revert restores matching size", func(t *testing.T) {
		statedb.SetCode(addr, []byte{1, 2, 3, 4})
		assertInvariant(t)
		rev := statedb.Snapshot()
		statedb.SetCode(addr, make([]byte, params.MaxCodeSize))
		assertInvariant(t)
		statedb.RevertToSnapshot(rev)
		assertInvariant(t)
		require.Equal(t, 4, statedb.GetCodeSize(addr))
	})

	t.Run("recreate clears code and size together", func(t *testing.T) {
		statedb.CreateAccount(addr)
		statedb.SetCode(addr, []byte("code"))
		statedb.AddBalance(addr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
		assertInvariant(t)

		// CreateAccount clears prior code/state for the account.
		statedb.CreateAccount(addr)
		assertInvariant(t)
		require.Equal(t, 0, statedb.GetCodeSize(addr))
		require.Nil(t, statedb.GetCode(addr))
	})
}
