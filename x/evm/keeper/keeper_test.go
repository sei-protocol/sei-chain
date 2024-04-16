package keeper_test

import (
	"math"
	"sync"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/rand"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestPurgePrefixNotHang(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	for i := 0; i < 50; i++ {
		ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
		store := k.PrefixStore(ctx, types.StateKey(evmAddr))
		store.Set([]byte{0x03}, []byte("test"))
	}
	require.NotPanics(t, func() { k.PurgePrefix(ctx, types.StateKey(evmAddr)) })
}

func TestGetChainID(t *testing.T) {
	k, _ := keeper.MockEVMKeeper()
	require.Equal(t, config.DefaultConfig.ChainID, k.ChainID().Int64())
}

func TestGetVMBlockContext(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	moduleAddr := k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName)
	evmAddr, _ := k.GetEVMAddress(ctx, moduleAddr)
	k.DeleteAddressMapping(ctx, moduleAddr, evmAddr)
	_, err := k.GetVMBlockContext(ctx, 0)
	require.NotNil(t, err)
}

func TestGetHashFn(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	f := k.GetHashFn(ctx)
	require.Equal(t, common.Hash{}, f(math.MaxInt64+1))
	require.Equal(t, common.BytesToHash(ctx.HeaderHash()), f(uint64(ctx.BlockHeight())))
	require.Equal(t, common.Hash{}, f(uint64(ctx.BlockHeight())+1))
	require.Equal(t, common.Hash{}, f(uint64(ctx.BlockHeight())-1))
}

func TestKeeper_CalculateNextNonce(t *testing.T) {
	address1 := common.BytesToAddress([]byte("addr1"))
	key1 := tmtypes.TxKey(rand.NewRand().Bytes(32))
	key2 := tmtypes.TxKey(rand.NewRand().Bytes(32))
	tests := []struct {
		name          string
		address       common.Address
		pending       bool
		setup         func(ctx sdk.Context, k *evmkeeper.Keeper)
		expectedNonce uint64
	}{
		{
			name:          "latest block, no latest stored",
			address:       address1,
			pending:       false,
			expectedNonce: 0,
		},
		{
			name:    "latest block, latest stored",
			address: address1,
			pending: false,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
			},
			expectedNonce: 50,
		},
		{
			name:    "latest block, latest stored with pending nonces",
			address: address1,
			pending: false,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				// because pending:false, these won't matter
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
			},
			expectedNonce: 50,
		},
		{
			name:    "pending block, nonce should follow the last pending",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
			},
			expectedNonce: 52,
		},
		{
			name:    "pending block, nonce should be the value of hole",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				// missing 51, so nonce = 51
				k.AddPendingNonce(key2, address1, 52, 0)
			},
			expectedNonce: 51,
		},
		{
			name:    "pending block, completed nonces should also be skipped",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
				k.SetNonce(ctx, address1, 52)
				k.RemovePendingNonce(key1)
				k.RemovePendingNonce(key2)
			},
			expectedNonce: 52,
		},
		{
			name:    "pending block, hole created by expiration",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 50, 0)
				k.AddPendingNonce(key2, address1, 51, 0)
				k.RemovePendingNonce(key1)
			},
			expectedNonce: 50,
		},
		{
			name:    "pending block, skipped nonces all in pending",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				// next expected for latest is 50, but 51,52 were sent
				k.SetNonce(ctx, address1, 50)
				k.AddPendingNonce(key1, address1, 51, 0)
				k.AddPendingNonce(key2, address1, 52, 0)
			},
			expectedNonce: 50,
		},
		{
			name:    "try 1000 nonces concurrently",
			address: address1,
			pending: true,
			setup: func(ctx sdk.Context, k *evmkeeper.Keeper) {
				// next expected for latest is 50, but 51,52 were sent
				k.SetNonce(ctx, address1, 50)
				wg := sync.WaitGroup{}
				for i := 50; i < 1000; i++ {
					wg.Add(1)
					go func(nonce int) {
						defer wg.Done()
						key := tmtypes.TxKey(rand.NewRand().Bytes(32))
						// call this just to exercise locks
						k.CalculateNextNonce(ctx, address1, true)
						k.AddPendingNonce(key, address1, uint64(nonce), 0)
					}(i)
				}
				wg.Wait()
			},
			expectedNonce: 1000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k, ctx := keeper.MockEVMKeeper()
			if test.setup != nil {
				test.setup(ctx, k)
			}
			next := k.CalculateNextNonce(ctx, test.address, test.pending)
			require.Equal(t, test.expectedNonce, next)
		})
	}
}

func TestDeferredInfo(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	ctx = ctx.WithTxIndex(1)
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{1, 2, 3}, common.Hash{4, 5, 6}, sdk.NewInt(1))
	ctx = ctx.WithTxIndex(2)
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{7, 8}, common.Hash{9, 0}, sdk.NewInt(1))
	ctx = ctx.WithTxIndex(3) // should be ignored because txResult has non-zero code
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{11, 12}, common.Hash{13, 14}, sdk.NewInt(1))
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}, {Code: 1}})
	infoList := k.GetEVMTxDeferredInfo(ctx)
	require.Equal(t, 2, len(infoList))
	require.Equal(t, 1, infoList[0].TxIndx)
	require.Equal(t, ethtypes.Bloom{1, 2, 3}, infoList[0].TxBloom)
	require.Equal(t, common.Hash{4, 5, 6}, infoList[0].TxHash)
	require.Equal(t, sdk.NewInt(1), infoList[0].Surplus)
	require.Equal(t, 2, infoList[1].TxIndx)
	require.Equal(t, ethtypes.Bloom{7, 8}, infoList[1].TxBloom)
	require.Equal(t, common.Hash{9, 0}, infoList[1].TxHash)
	require.Equal(t, sdk.NewInt(1), infoList[1].Surplus)
	// test clear tx deferred info
	k.ClearEVMTxDeferredInfo()
	infoList = k.GetEVMTxDeferredInfo(ctx)
	require.Empty(t, len(infoList))
}

func TestAddPendingNonce(t *testing.T) {
	k, _ := keeper.MockEVMKeeper()
	k.AddPendingNonce(tmtypes.TxKey{1}, common.HexToAddress("123"), 1, 1)
	k.AddPendingNonce(tmtypes.TxKey{2}, common.HexToAddress("123"), 2, 1)
	k.AddPendingNonce(tmtypes.TxKey{3}, common.HexToAddress("123"), 2, 2) // should replace the one above
	pendingTxs := k.GetPendingTxs()[common.HexToAddress("123").Hex()]
	require.Equal(t, 2, len(pendingTxs))
	require.Equal(t, tmtypes.TxKey{1}, pendingTxs[0].Key)
	require.Equal(t, uint64(1), pendingTxs[0].Nonce)
	require.Equal(t, int64(1), pendingTxs[0].Priority)
	require.Equal(t, tmtypes.TxKey{3}, pendingTxs[1].Key)
	require.Equal(t, uint64(2), pendingTxs[1].Nonce)
	require.Equal(t, int64(2), pendingTxs[1].Priority)
	keyToNonce := k.GetKeysToNonces()
	require.Equal(t, common.HexToAddress("123"), keyToNonce[tmtypes.TxKey{1}].Address)
	require.Equal(t, uint64(1), keyToNonce[tmtypes.TxKey{1}].Nonce)
	require.Equal(t, common.HexToAddress("123"), keyToNonce[tmtypes.TxKey{3}].Address)
	require.Equal(t, uint64(2), keyToNonce[tmtypes.TxKey{3}].Nonce)
	require.NotContains(t, keyToNonce, tmtypes.TxKey{2})
}
