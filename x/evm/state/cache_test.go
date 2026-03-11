package state_test

import (
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

type countingKeeper struct {
	*evmkeeper.Keeper
	stateReads    int
	nonceReads    int
	codeHashReads int
	codeReads     int
	codeSizeReads int
	balanceReads  int
}

func (k *countingKeeper) GetState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	k.stateReads++
	return k.Keeper.GetState(ctx, addr, hash)
}

func (k *countingKeeper) GetNonce(ctx sdk.Context, addr common.Address) uint64 {
	k.nonceReads++
	return k.Keeper.GetNonce(ctx, addr)
}

func (k *countingKeeper) GetCodeHash(ctx sdk.Context, addr common.Address) common.Hash {
	k.codeHashReads++
	return k.Keeper.GetCodeHash(ctx, addr)
}

func (k *countingKeeper) GetCode(ctx sdk.Context, addr common.Address) []byte {
	k.codeReads++
	return k.Keeper.GetCode(ctx, addr)
}

func (k *countingKeeper) GetCodeSize(ctx sdk.Context, addr common.Address) int {
	k.codeSizeReads++
	return k.Keeper.GetCodeSize(ctx, addr)
}

func (k *countingKeeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	k.balanceReads++
	return k.Keeper.GetBalance(ctx, addr)
}

func TestReadCacheAvoidsRepeatedKeeperReads(t *testing.T) {
	baseKeeper := &testkeeper.EVMTestApp.EvmKeeper
	k := &countingKeeper{Keeper: baseKeeper}
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now()).WithIsTracing(true)
	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("storage-key"))
	val := common.BytesToHash([]byte("storage-value"))

	k.SetState(ctx, evmAddr, key, val)
	k.SetCode(ctx, evmAddr, []byte("contract code"))
	k.SetNonce(ctx, evmAddr, 7)

	stateDB := state.NewDBImpl(ctx, k, false)
	stateDB.AddBalance(evmAddr, uint256.NewInt(12345), tracing.BalanceChangeUnspecified)

	require.Equal(t, val, stateDB.GetState(evmAddr, key))
	require.Equal(t, val, stateDB.GetState(evmAddr, key))
	require.Equal(t, 1, k.stateReads)

	require.Equal(t, uint64(7), stateDB.GetNonce(evmAddr))
	require.Equal(t, uint64(7), stateDB.GetNonce(evmAddr))
	require.Equal(t, 1, k.nonceReads)

	require.NotEqual(t, common.Hash{}, stateDB.GetCodeHash(evmAddr))
	require.NotEqual(t, common.Hash{}, stateDB.GetCodeHash(evmAddr))
	require.Equal(t, 1, k.codeHashReads)

	require.Equal(t, []byte("contract code"), stateDB.GetCode(evmAddr))
	require.Equal(t, []byte("contract code"), stateDB.GetCode(evmAddr))
	require.Equal(t, 1, k.codeReads)

	require.Equal(t, len([]byte("contract code")), stateDB.GetCodeSize(evmAddr))
	require.Equal(t, len([]byte("contract code")), stateDB.GetCodeSize(evmAddr))
	require.Equal(t, 1, k.codeSizeReads)

	require.Equal(t, uint256.NewInt(12345), stateDB.GetBalance(evmAddr))
	require.Equal(t, uint256.NewInt(12345), stateDB.GetBalance(evmAddr))
	require.Equal(t, 1, k.balanceReads)
}

func TestCleanupForTracerPreservesCurrentReadCache(t *testing.T) {
	baseKeeper := &testkeeper.EVMTestApp.EvmKeeper
	k := &countingKeeper{Keeper: baseKeeper}
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now()).WithIsTracing(true)
	_, evmAddr := testkeeper.MockAddressPair()
	key := common.BytesToHash([]byte("cleanup-key"))
	val := common.BytesToHash([]byte("cleanup-value"))

	k.SetState(ctx, evmAddr, key, val)

	stateDB := state.NewDBImpl(ctx, k, true)
	require.Equal(t, val, stateDB.GetState(evmAddr, key))
	require.Equal(t, 1, k.stateReads)

	stateDB.CleanupForTracer()
	require.Equal(t, val, stateDB.GetState(evmAddr, key))
	require.Equal(t, 1, k.stateReads)
}
