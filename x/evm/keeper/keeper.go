package keeper

import (
	"math"
	"math/big"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	tmtypes "github.com/tendermint/tendermint/types"
)

type Keeper struct {
	storeKey   sdk.StoreKey
	Paramstore paramtypes.Subspace

	bankKeeper    bankkeeper.Keeper
	accountKeeper *authkeeper.AccountKeeper
	stakingKeeper *stakingkeeper.Keeper

	cachedFeeCollectorAddressMtx *sync.RWMutex
	cachedFeeCollectorAddress    *common.Address
	evmTxIndicesMtx              *sync.Mutex
	evmTxIndices                 []int

	evmTxCountsMtx              *sync.RWMutex
	evmTxCountsIncludingPending map[string]uint64 // hex address to count; only written in CheckTx and read in RPC
}

func NewKeeper(
	storeKey sdk.StoreKey, paramstore paramtypes.Subspace,
	bankKeeper bankkeeper.Keeper, accountKeeper *authkeeper.AccountKeeper, stakingKeeper *stakingkeeper.Keeper) *Keeper {
	if !paramstore.HasKeyTable() {
		paramstore = paramstore.WithKeyTable(types.ParamKeyTable())
	}
	k := &Keeper{
		storeKey:                     storeKey,
		Paramstore:                   paramstore,
		bankKeeper:                   bankKeeper,
		accountKeeper:                accountKeeper,
		stakingKeeper:                stakingKeeper,
		cachedFeeCollectorAddressMtx: &sync.RWMutex{},
		evmTxIndicesMtx:              &sync.Mutex{},
		evmTxIndices:                 []int{},
		evmTxCountsIncludingPending:  map[string]uint64{},
		evmTxCountsMtx:               &sync.RWMutex{},
	}
	return k
}

func (k *Keeper) AccountKeeper() *authkeeper.AccountKeeper {
	return k.accountKeeper
}

func (k *Keeper) BankKeeper() bankkeeper.Keeper {
	return k.bankKeeper
}

func (k *Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}

func (k *Keeper) PrefixStore(ctx sdk.Context, pref []byte) sdk.KVStore {
	store := ctx.KVStore(k.GetStoreKey())
	return prefix.NewStore(store, pref)
}

func (k *Keeper) PurgePrefix(ctx sdk.Context, pref []byte) {
	store := k.PrefixStore(ctx, pref)
	iter := store.Iterator(nil, nil)
	keys := [][]byte{}
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	iter.Close()
	for _, key := range keys {
		store.Delete(key)
	}
}

func (k *Keeper) GetVMBlockContext(ctx sdk.Context, gp core.GasPool) (*vm.BlockContext, error) {
	coinbase, err := k.GetFeeCollectorAddress(ctx)
	if err != nil {
		return nil, err
	}
	r, err := ctx.BlockHeader().Time.MarshalBinary()
	if err != nil {
		return nil, err
	}
	rh := common.BytesToHash(r)
	return &vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     k.GetHashFn(ctx),
		Coinbase:    coinbase,
		GasLimit:    gp.Gas(),
		BlockNumber: big.NewInt(ctx.BlockHeight()),
		Time:        uint64(ctx.BlockHeader().Time.Unix()),
		Difficulty:  big.NewInt(0),                               // only needed for PoW
		BaseFee:     k.GetBaseFeePerGas(ctx).RoundInt().BigInt(), // feemarket not enabled
		Random:      &rh,
	}, nil
}

// returns a function that provides block header hash based on block number
func (k *Keeper) GetHashFn(ctx sdk.Context) vm.GetHashFunc {
	return func(height uint64) common.Hash {
		if height > math.MaxInt64 {
			ctx.Logger().Error("Sei block height is bounded by int64 range")
			return common.Hash{}
		}
		h := int64(height)
		if ctx.BlockHeight() == h {
			// current header hash is in the context already
			return common.BytesToHash(ctx.HeaderHash())
		}
		if ctx.BlockHeight() < h {
			// future block doesn't have a hash yet
			return common.Hash{}
		}
		// fetch historical hash from historical info
		return k.getHistoricalHash(ctx, h)
	}
}

func (k *Keeper) ClearEVMTxIndices() {
	// no need to acquire mutex here since it's only called by BeginBlock
	k.evmTxIndices = []int{}
}

func (k *Keeper) GetEVMTxIndices() []int {
	// no need to acquire mutex here since it's only called by EndBlock
	return k.evmTxIndices
}

func (k *Keeper) AppendToEVMTxIndices(idx int) {
	k.evmTxIndicesMtx.Lock()
	defer k.evmTxIndicesMtx.Unlock()
	k.evmTxIndices = append(k.evmTxIndices, idx)
}

func (k *Keeper) getHistoricalHash(ctx sdk.Context, h int64) common.Hash {
	histInfo, found := k.stakingKeeper.GetHistoricalInfo(ctx, h)
	if !found {
		// too old, already pruned
		return common.Hash{}
	}
	header, _ := tmtypes.HeaderFromProto(&histInfo.Header)

	return common.BytesToHash(header.Hash())
}

func (k *Keeper) IncrementPendingTxCount(addr common.Address) {
	k.evmTxCountsMtx.Lock()
	defer k.evmTxCountsMtx.Unlock()
	addrStr := addr.Hex()
	if cnt, ok := k.evmTxCountsIncludingPending[addrStr]; ok {
		k.evmTxCountsIncludingPending[addrStr] = cnt + 1
		return
	}
	k.evmTxCountsIncludingPending[addrStr] = 1
}

func (k *Keeper) DecrementPendingTxCount(addr common.Address) {
	k.evmTxCountsMtx.Lock()
	defer k.evmTxCountsMtx.Unlock()
	addrStr := addr.Hex()
	if cnt, ok := k.evmTxCountsIncludingPending[addrStr]; ok && cnt > 0 {
		k.evmTxCountsIncludingPending[addrStr] = cnt - 1
	}
}

func (k *Keeper) GetPendingTxCount(addr common.Address) uint64 {
	k.evmTxCountsMtx.RLock()
	defer k.evmTxCountsMtx.RUnlock()
	if cnt, ok := k.evmTxCountsIncludingPending[addr.Hex()]; ok {
		return cnt
	}
	return 0
}
