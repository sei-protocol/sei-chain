package keeper

import (
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
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type Keeper struct {
	storeKey    sdk.StoreKey
	memStoreKey sdk.StoreKey
	Paramstore  paramtypes.Subspace

	deferredInfo *sync.Map
	txResults    []*abci.ExecTxResult

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
		deferredInfo:                 &sync.Map{},
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
	k.deferredInfo.Range(func(key, value any) bool {
		txIdx := key.(int)
		if txIdx < 0 || txIdx >= len(k.txResults) {
			ctx.Logger().Error(fmt.Sprintf("getting invalid tx index in EVM deferred info: %d, num of txs: %d", txIdx, len(k.txResults)))
			return true
		}
		if k.txResults[txIdx].Code == 0 {
			res = append(res, *(value.(*EvmTxDeferredInfo)))
		}
		return true
	})
	sort.SliceStable(res, func(i, j int) bool { return res[i].TxIndx < res[j].TxIndx })
	return
}

func (k *Keeper) AppendToEvmTxDeferredInfo(ctx sdk.Context, bloom ethtypes.Bloom, txHash common.Hash) {
	k.deferredInfo.Store(ctx.TxIndex(), &EvmTxDeferredInfo{
		TxIndx:  ctx.TxIndex(),
		TxBloom: bloom,
		TxHash:  txHash,
	})
}

func (k *Keeper) ClearEVMTxDeferredInfo() {
	k.deferredInfo = &sync.Map{}
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

func (k *Keeper) SetTxResults(txResults []*abci.ExecTxResult) {
	k.txResults = txResults
}

func uint64Cmp(a, b uint64) int {
	if a < b {
		return -1
	} else if a == b {
		return 0
	}
	return 1
}
