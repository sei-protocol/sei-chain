package state_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestExist(t *testing.T) {
	// not exist
	k, ctx := testkeeper.MockEVMKeeper()
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	require.False(t, statedb.Exist(addr))

	// has code
	_, addr2 := testkeeper.MockAddressPair()
	statedb.SetCode(addr2, []byte{3})
	require.True(t, statedb.Exist(addr2))

	// destructed
	_, addr3 := testkeeper.MockAddressPair()
	statedb.SelfDestruct(addr3)
	require.True(t, statedb.Exist(addr3))
}

func TestEmpty(t *testing.T) {
	// empty
	k, ctx := testkeeper.MockEVMKeeper()
	_, addr := testkeeper.MockAddressPair()
	statedb := state.NewDBImpl(ctx, k, false)
	require.True(t, statedb.Empty(addr))

	// has balance
	k.BankKeeper().MintCoins(statedb.Ctx(), types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1))))
	k.BankKeeper().SendCoinsFromModuleToAccount(statedb.Ctx(), types.ModuleName, state.GetMiddleManAddress(ctx.TxIndex()), sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1))))
	statedb.AddBalance(addr, big.NewInt(1000000000000))
	require.False(t, statedb.Empty(addr))

	// has non-zero nonce
	statedb.SubBalance(addr, big.NewInt(1000000000000))
	statedb.SetNonce(addr, 1)
	require.False(t, statedb.Empty(addr))

	// has code
	statedb.SetNonce(addr, 0)
	statedb.SetCode(addr, []byte{1})
	require.False(t, statedb.Empty(addr))
}
