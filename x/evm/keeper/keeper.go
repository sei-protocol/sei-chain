package keeper

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type Keeper struct {
	storeKey    sdk.StoreKey
	memStoreKey sdk.StoreKey
	Paramstore  paramtypes.Subspace

	bankKeeper    bankkeeper.Keeper
	accountKeeper *authkeeper.AccountKeeper
	stakingKeeper *stakingkeeper.Keeper

	cachedFeeCollectorAddressMtx *sync.RWMutex
	cachedFeeCollectorAddress    *common.Address
	nonceMx                      *sync.RWMutex
	pendingNonces                map[string][]uint64
	keyToNonce                   map[tmtypes.TxKey]*addressNoncePair
}

type EvmTxDeferredInfo struct {
	TxIndx  int
	TxHash  common.Hash
	TxBloom ethtypes.Bloom
}

type addressNoncePair struct {
	address common.Address
	nonce   uint64
}

func NewKeeper(
	storeKey sdk.StoreKey, memStoreKey sdk.StoreKey, paramstore paramtypes.Subspace,
	bankKeeper bankkeeper.Keeper, accountKeeper *authkeeper.AccountKeeper, stakingKeeper *stakingkeeper.Keeper) *Keeper {
	if !paramstore.HasKeyTable() {
		paramstore = paramstore.WithKeyTable(types.ParamKeyTable())
	}
	k := &Keeper{
		storeKey:                     storeKey,
		memStoreKey:                  memStoreKey,
		Paramstore:                   paramstore,
		bankKeeper:                   bankKeeper,
		accountKeeper:                accountKeeper,
		stakingKeeper:                stakingKeeper,
		pendingNonces:                make(map[string][]uint64),
		nonceMx:                      &sync.RWMutex{},
		cachedFeeCollectorAddressMtx: &sync.RWMutex{},
		keyToNonce:                   make(map[tmtypes.TxKey]*addressNoncePair),
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
	if err := store.DeleteAll(nil, nil); err != nil {
		panic(err)
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

func (k *Keeper) GetEVMTxDeferredInfo(ctx sdk.Context) (res []EvmTxDeferredInfo) {
	hashMap, bloomMap := map[int]common.Hash{}, map[int]ethtypes.Bloom{}
	hashIter := prefix.NewStore(ctx.KVStore(k.memStoreKey), types.TxHashPrefix).Iterator(nil, nil)
	for ; hashIter.Valid(); hashIter.Next() {
		h := common.Hash{}
		h.SetBytes(hashIter.Value())
		hashMap[int(binary.BigEndian.Uint32(hashIter.Key()))] = h
	}
	hashIter.Close()
	bloomIter := prefix.NewStore(ctx.KVStore(k.memStoreKey), types.TxBloomPrefix).Iterator(nil, nil)
	for ; bloomIter.Valid(); bloomIter.Next() {
		b := ethtypes.Bloom{}
		b.SetBytes(bloomIter.Value())
		bloomMap[int(binary.BigEndian.Uint32(bloomIter.Key()))] = b
	}
	bloomIter.Close()
	for idx, h := range hashMap {
		i := EvmTxDeferredInfo{TxIndx: idx, TxHash: h}
		if b, ok := bloomMap[idx]; ok {
			i.TxBloom = b
			delete(bloomMap, idx)
		}
		res = append(res, i)
	}
	for idx, b := range bloomMap {
		res = append(res, EvmTxDeferredInfo{TxIndx: idx, TxBloom: b})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].TxIndx < res[j].TxIndx })
	return
}

func (k *Keeper) AppendToEvmTxDeferredInfo(ctx sdk.Context, bloom ethtypes.Bloom, txHash common.Hash) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint32(key, uint32(ctx.TxIndex()))
	prefix.NewStore(ctx.KVStore(k.memStoreKey), types.TxHashPrefix).Set(key, txHash[:])
	prefix.NewStore(ctx.KVStore(k.memStoreKey), types.TxBloomPrefix).Set(key, bloom[:])
}

func (k *Keeper) ClearEVMTxDeferredInfo(ctx sdk.Context) {
	hashStore := prefix.NewStore(ctx.KVStore(k.memStoreKey), types.TxHashPrefix)
	hashIterator := hashStore.Iterator(nil, nil)
	defer hashIterator.Close()
	hashKeysToDelete := [][]byte{}
	for ; hashIterator.Valid(); hashIterator.Next() {
		hashKeysToDelete = append(hashKeysToDelete, hashIterator.Key())
	}
	// close the first iterator for safety
	hashIterator.Close()
	for _, key := range hashKeysToDelete {
		hashStore.Delete(key)
	}

	bloomStore := prefix.NewStore(ctx.KVStore(k.memStoreKey), types.TxBloomPrefix)
	bloomIterator := bloomStore.Iterator(nil, nil)
	bloomKeysToDelete := [][]byte{}
	defer bloomIterator.Close()
	for ; bloomIterator.Valid(); bloomIterator.Next() {
		bloomKeysToDelete = append(bloomKeysToDelete, bloomIterator.Key())
	}
	// close the second iterator for safety
	bloomIterator.Close()
	for _, key := range bloomKeysToDelete {
		bloomStore.Delete(key)
	}
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

// CalculateNextNonce calculates the next nonce for an address
// If includePending is true, it will consider pending nonces
// If includePending is false, it will only return the next nonce from GetNonce
func (k *Keeper) CalculateNextNonce(ctx sdk.Context, addr common.Address, includePending bool) uint64 {
	k.nonceMx.Lock()
	defer k.nonceMx.Unlock()

	nextNonce := k.GetNonce(ctx, addr)

	// we only want the latest nonce if we're not including pending
	if !includePending {
		return nextNonce
	}

	// get the pending nonces (nil is fine)
	pending := k.pendingNonces[addr.Hex()]

	// Check each nonce starting from latest until we find a gap
	// That gap is the next nonce we should use.
	for ; ; nextNonce++ {
		// if it's not in pending, then it's the next nonce
		if _, found := sort.Find(len(pending), func(i int) int { return uint64Cmp(nextNonce, pending[i]) }); !found {
			return nextNonce
		}
	}
}

// AddPendingNonce adds a pending nonce to the keeper
func (k *Keeper) AddPendingNonce(key tmtypes.TxKey, addr common.Address, nonce uint64) {
	k.nonceMx.Lock()
	defer k.nonceMx.Unlock()

	addrStr := addr.Hex()
	if existing, ok := k.keyToNonce[key]; ok {
		if existing.nonce != nonce {
			fmt.Printf("Seeing transactions with the same hash %X but different nonces (%d vs. %d), which should be impossible\n", key, nonce, existing.nonce)
		}
		if existing.address != addr {
			fmt.Printf("Seeing transactions with the same hash %X but different addresses (%s vs. %s), which should be impossible\n", key, addr.Hex(), existing.address.Hex())
		}
		// we want to no-op whether it's a genuine duplicate or not
		return
	}
	k.keyToNonce[key] = &addressNoncePair{
		address: addr,
		nonce:   nonce,
	}
	k.pendingNonces[addrStr] = append(k.pendingNonces[addrStr], nonce)
	slices.Sort(k.pendingNonces[addrStr])
}

// RemovePendingNonce removes a pending nonce from the keeper but leaves a hole
// so that a future transaction must use this nonce.
func (k *Keeper) RemovePendingNonce(key tmtypes.TxKey) {
	k.nonceMx.Lock()
	defer k.nonceMx.Unlock()
	tx, ok := k.keyToNonce[key]

	if !ok {
		return
	}

	delete(k.keyToNonce, key)

	addr := tx.address.Hex()
	pendings := k.pendingNonces[addr]
	firstMatch, found := sort.Find(len(pendings), func(i int) int { return uint64Cmp(tx.nonce, pendings[i]) })
	if !found {
		fmt.Printf("Removing tx %X without a corresponding pending nonce, which should not happen\n", key)
		return
	}
	k.pendingNonces[addr] = append(k.pendingNonces[addr][:firstMatch], k.pendingNonces[addr][firstMatch+1:]...)
	if len(k.pendingNonces[addr]) == 0 {
		delete(k.pendingNonces, addr)
	}
}

func uint64Cmp(a, b uint64) int {
	if a < b {
		return -1
	} else if a == b {
		return 0
	}
	return 1
}
