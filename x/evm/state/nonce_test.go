package state_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/stretchr/testify/require"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func TestNonce(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	stateDB := state.NewDBImpl(ctx, k, false)
	_, addr := testkeeper.MockAddressPair()
	stateDB.SetNonce(addr, 1, tracing.NonceChangeEoACall)
	nonce := stateDB.GetNonce(addr)
	require.Equal(t, nonce, uint64(1))
	stateDB.SetNonce(addr, 2, tracing.NonceChangeEoACall)
	nonce = stateDB.GetNonce(addr)
	require.Equal(t, nonce, uint64(2))
}

func TestSetNonceCallsV2Logger(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	stateDB := state.NewDBImpl(ctx, k, false)
	_, addr := testkeeper.MockAddressPair()
	stateDB.SetNonce(addr, 7, tracing.NonceChangeEoACall)

	var (
		gotAddr   common.Address
		gotPrev   uint64
		gotNew    uint64
		gotReason tracing.NonceChangeReason
	)
	stateDB.SetLogger(&tracing.Hooks{
		OnNonceChangeV2: func(addr common.Address, prev, new uint64, reason tracing.NonceChangeReason) {
			gotAddr = addr
			gotPrev = prev
			gotNew = new
			gotReason = reason
		},
	})

	stateDB.SetNonce(addr, 9, tracing.NonceChangeContractCreator)

	require.Equal(t, addr, gotAddr)
	require.EqualValues(t, 7, gotPrev)
	require.EqualValues(t, 9, gotNew)
	require.Equal(t, tracing.NonceChangeContractCreator, gotReason)
}
