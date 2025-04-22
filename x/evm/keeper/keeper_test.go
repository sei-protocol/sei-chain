package keeper_test

import (
	"context"
	"encoding/hex"
	"math"
	"math/big"
	"sync"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
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
	k, ctx := keeper.MockEVMKeeper()
	require.Equal(t, config.DefaultChainID, k.ChainID(ctx).Int64())

	ctx = ctx.WithChainID("pacific-1")
	require.Equal(t, int64(1329), k.ChainID(ctx).Int64())

	ctx = ctx.WithChainID("atlantic-2")
	require.Equal(t, int64(1328), k.ChainID(ctx).Int64())

	ctx = ctx.WithChainID("arctic-1")
	require.Equal(t, int64(713715), k.ChainID(ctx).Int64())
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
	a := app.Setup(false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{})
	ctx = ctx.WithTxIndex(1)
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{1, 2, 3}, common.Hash{4, 5, 6}, sdk.NewInt(1))
	ctx = ctx.WithTxIndex(2)
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{7, 8}, common.Hash{9, 0}, sdk.NewInt(1))
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}, {Code: 1, Log: "test error"}})
	msg := mockEVMTransactionMessage(t)
	k.SetMsgs([]*types.MsgEVMTransaction{nil, {}, {}, msg})
	infoList := k.GetAllEVMTxDeferredInfo(ctx)
	require.Equal(t, 3, len(infoList))
	require.Equal(t, uint32(1), infoList[0].TxIndex)
	require.Equal(t, ethtypes.Bloom{1, 2, 3}, ethtypes.BytesToBloom(infoList[0].TxBloom))
	require.Equal(t, common.Hash{4, 5, 6}, common.BytesToHash(infoList[0].TxHash))
	require.Equal(t, sdk.NewInt(1), infoList[0].Surplus)
	require.Equal(t, uint32(2), infoList[1].TxIndex)
	require.Equal(t, ethtypes.Bloom{7, 8}, ethtypes.BytesToBloom(infoList[1].TxBloom))
	require.Equal(t, common.Hash{9, 0}, common.BytesToHash(infoList[1].TxHash))
	require.Equal(t, sdk.NewInt(1), infoList[1].Surplus)
	require.Equal(t, uint32(3), infoList[2].TxIndex)
	require.Equal(t, ethtypes.Bloom{}, ethtypes.BytesToBloom(infoList[2].TxBloom))
	etx, _ := msg.AsTransaction()
	require.Equal(t, etx.Hash(), common.BytesToHash(infoList[2].TxHash))
	require.Equal(t, "test error", infoList[2].Error)
	// test clear tx deferred info
	a.SetDeliverStateToCommit()
	a.Commit(context.Background()) // commit would clear transient stores
	k.SetTxResults([]*abci.ExecTxResult{})
	k.SetMsgs([]*types.MsgEVMTransaction{})
	infoList = k.GetAllEVMTxDeferredInfo(ctx)
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

func TestGetCustomPrecompiles(t *testing.T) {
	k, ctx := keeper.MockEVMKeeperPrecompiles()
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(139936278), "v6.0.5")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(129965597), "v6.0.3")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(126326956), "v6.0.2")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(119821526), "v6.0.1")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(114945913), "v6.0.0")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(107000672), "v5.9.0")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(102491599), "v5.8.0")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(94496767), "v5.7.5")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(89475838), "v5.6.2")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(84006014), "v5.5.5")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(79123881), "v5.5.2")
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(73290488), "v3.9.0")
	ps := k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(139936279))
	for _, v := range ps {
		require.Equal(t, "v6.0.5", v)
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(129965598))
	for addr, v := range ps {
		switch addr.Hex() {
		case pointerview.PointerViewAddress:
			require.Equal(t, "v5.6.2", v)
		case distribution.DistrAddress:
		case gov.GovAddress:
		case staking.StakingAddress:
			require.Equal(t, "v5.8.0", v)
		case pointer.PointerAddress:
		case wasmd.WasmdAddress:
			require.Equal(t, "v6.0.0", v)
		default:
			require.Equal(t, "v6.0.3", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(126326957))
	for addr, v := range ps {
		switch addr.Hex() {
		case pointerview.PointerViewAddress:
		case json.JSONAddress:
			require.Equal(t, "v5.6.2", v)
		case distribution.DistrAddress:
		case gov.GovAddress:
		case staking.StakingAddress:
			require.Equal(t, "v5.8.0", v)
		case pointer.PointerAddress:
		case wasmd.WasmdAddress:
			require.Equal(t, "v6.0.0", v)
		default:
			require.Equal(t, "v6.0.1", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(119821527))
	for a, v := range ps {
		switch a.Hex() {
		case json.JSONAddress:
		case pointerview.PointerViewAddress:
			require.Equal(t, "v5.6.2", v)
		case distribution.DistrAddress:
		case gov.GovAddress:
		case staking.StakingAddress:
		case ibc.IBCAddress, bank.BankAddress, oracle.OracleAddress, addr.AddrAddress:
			require.Equal(t, "v6.0.1", v)
		default:
			require.Equal(t, "v6.0.0", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(114945914))
	for addr, v := range ps {
		switch addr.Hex() {
		case json.JSONAddress:
		case pointerview.PointerViewAddress:
			require.Equal(t, "v5.6.2", v)
		case distribution.DistrAddress:
		case ibc.IBCAddress:
		case gov.GovAddress:
		case staking.StakingAddress:
			require.Equal(t, "v5.8.0", v)
		default:
			require.Equal(t, "v6.0.0", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(107000673))
	for a, v := range ps {
		switch a.Hex() {
		case addr.AddrAddress:
			require.Equal(t, "v5.7.5", v)
		case json.JSONAddress:
		case oracle.OracleAddress:
		case pointerview.PointerViewAddress:
			require.Equal(t, "v5.6.2", v)
		default:
			require.Equal(t, "v5.8.0", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(102491600))
	for a, v := range ps {
		switch a.Hex() {
		case addr.AddrAddress:
			require.Equal(t, "v5.7.5", v)
		case json.JSONAddress:
		case oracle.OracleAddress:
		case pointerview.PointerViewAddress:
			require.Equal(t, "v5.6.2", v)
		default:
			require.Equal(t, "v5.8.0", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(94496768))
	for a, v := range ps {
		switch a.Hex() {
		case addr.AddrAddress:
		case pointer.PointerAddress:
		case wasmd.WasmdAddress:
			require.Equal(t, "v5.7.5", v)
		default:
			require.Equal(t, "v5.6.2", v)
		}
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(89475839))
	for _, v := range ps {
		require.Equal(t, "v5.6.2", v)
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(84006015))
	for _, v := range ps {
		require.Equal(t, "v5.5.5", v)
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(79123882))
	for _, v := range ps {
		require.Equal(t, "v5.5.2", v)
	}
	ps = k.GetCustomPrecompilesVersions(ctx.WithBlockHeight(73290489))
	require.Len(t, ps, 0)
}

func mockEVMTransactionMessage(t *testing.T) *types.MsgEVMTransaction {
	k, ctx := testkeeper.MockEVMKeeper()
	chainID := k.ChainID(ctx)
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10000000000000),
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000000000000000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}

	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	return msg
}
