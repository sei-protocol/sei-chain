package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestFinalizeSweepsDestructedAccounts(t *testing.T) {
	k, baseCtx := testkeeper.MockEVMKeeper(t)
	baseCtx = baseCtx.WithBlockTime(time.Now())

	accts := make([]common.Address, 16)
	wantSpenders := make([]string, len(accts))
	for i := range accts {
		_, accts[i] = testkeeper.MockAddressPair()
		wantSpenders[i] = k.GetSeiAddressOrDefault(baseCtx, accts[i]).String()
	}

	ctx := baseCtx.WithMultiStore(baseCtx.MultiStore().CacheMultiStore()).WithEventManager(sdk.NewEventManager())
	statedb := state.NewDBImpl(ctx, k, false)
	for i, acc := range accts {
		statedb.CreateAccount(acc)
		statedb.SetCode(acc, []byte("code"), tracing.CodeChangeUnspecified)
		require.True(t, statedb.IsNewContract(acc))
		statedb.SelfDestruct(acc)
		residual := uint256.NewInt(1)
		if i%2 == 1 {
			residual = uint256.NewInt(999_999_999_999)
		}
		statedb.AddBalance(acc, residual, tracing.BalanceChangeTransfer)
	}
	_, err := statedb.Finalize()
	require.NoError(t, err)

	require.ElementsMatch(t, wantSpenders, weiSpenders(ctx.EventManager().Events()))
	for _, acc := range accts {
		require.Empty(t, k.GetCode(ctx, acc))
		require.Zero(t, k.GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, acc)).Sign())
	}
}

func weiSpenders(events sdk.Events) []string {
	var spenders []string
	for _, e := range events {
		if e.Type != banktypes.EventTypeWeiSpent {
			continue
		}
		for _, attr := range e.Attributes {
			if string(attr.Key) == banktypes.AttributeKeySpender {
				spenders = append(spenders, string(attr.Value))
			}
		}
	}
	return spenders
}
