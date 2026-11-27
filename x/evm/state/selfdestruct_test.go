package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

// EIP-8246: Finalize clears a self-destructed account's code and leaves its balance in place.
func TestFinalizeKeepsDestructedAccountBalances(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	baseCtx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())

	accts := make([]common.Address, 16)
	residuals := make([]*uint256.Int, len(accts))
	for i := range accts {
		_, accts[i] = testkeeper.MockAddressPair()
	}

	ctx := baseCtx.WithMultiStore(baseCtx.MultiStore().CacheMultiStore()).WithEventManager(sdk.NewEventManager())
	statedb := state.NewDBImpl(ctx, k, false)
	for i, acc := range accts {
		statedb.CreateAccount(acc)
		statedb.SetCode(acc, []byte("code"), tracing.CodeChangeUnspecified)
		require.True(t, statedb.IsNewContract(acc))
		statedb.SelfDestruct(acc)
		residuals[i] = uint256.NewInt(1)
		if i%2 == 1 {
			residuals[i] = uint256.NewInt(999_999_999_999)
		}
		statedb.AddBalance(acc, residuals[i], tracing.BalanceChangeTransfer)
	}
	_, err := statedb.Finalize()
	require.NoError(t, err)

	require.Empty(t, weiSpenders(ctx.EventManager().Events()))
	for i, acc := range accts {
		require.Empty(t, k.GetCode(ctx, acc))
		require.Equal(t, residuals[i].ToBig(), k.GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, acc)))
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
