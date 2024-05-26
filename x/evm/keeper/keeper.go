package keeper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"sync"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	ibctransferkeeper "github.com/cosmos/ibc-go/v3/modules/apps/transfer/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/tests"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
	"github.com/sei-protocol/sei-chain/x/evm/querier"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type Keeper struct {
	storeKey    sdk.StoreKey
	memStoreKey sdk.StoreKey
	Paramstore  paramtypes.Subspace

	deferredInfo *sync.Map
	txResults    []*abci.ExecTxResult

	bankKeeper     bankkeeper.Keeper
	accountKeeper  *authkeeper.AccountKeeper
	stakingKeeper  *stakingkeeper.Keeper
	transferKeeper ibctransferkeeper.Keeper
	wasmKeeper     *wasmkeeper.PermissionedKeeper
	wasmViewKeeper *wasmkeeper.Keeper

	cachedFeeCollectorAddressMtx *sync.RWMutex
	cachedFeeCollectorAddress    *common.Address
	nonceMx                      *sync.RWMutex
	pendingTxs                   map[string][]*PendingTx
	keyToNonce                   map[tmtypes.TxKey]*AddressNoncePair

	QueryConfig *querier.Config

	// only used during ETH replay. Not used in chain critical path.
	EthClient       *ethclient.Client
	EthReplayConfig replay.Config

	// only used during blocktest. Not used in chain critical path.
	EthBlockTestConfig blocktest.Config
	BlockTest          *tests.BlockTest

	// used for both ETH replay and block tests. Not used in chain critical path.
	Trie        ethstate.Trie
	DB          ethstate.Database
	Root        common.Hash
	ReplayBlock *ethtypes.Block
}

type EvmTxDeferredInfo struct {
	TxIndx  int
	TxHash  common.Hash
	TxBloom ethtypes.Bloom
	Surplus sdk.Int
	Error   string
}

type AddressNoncePair struct {
	Address common.Address
	Nonce   uint64
}

type PendingTx struct {
	Key      tmtypes.TxKey
	Nonce    uint64
	Priority int64
}

// only used during ETH replay
type ReplayChainContext struct {
	ethClient *ethclient.Client
}

func (ctx *ReplayChainContext) Engine() consensus.Engine {
	return nil
}

func (ctx *ReplayChainContext) GetHeader(hash common.Hash, number uint64) *ethtypes.Header {
	res, err := ctx.ethClient.BlockByNumber(context.Background(), big.NewInt(int64(number)))
	if err != nil || res.Header_.Hash() != hash {
		return nil
	}
	return res.Header_
}

func NewKeeper(
	storeKey sdk.StoreKey, memStoreKey sdk.StoreKey, paramstore paramtypes.Subspace,
	bankKeeper bankkeeper.Keeper, accountKeeper *authkeeper.AccountKeeper, stakingKeeper *stakingkeeper.Keeper,
	transferKeeper ibctransferkeeper.Keeper, wasmKeeper *wasmkeeper.PermissionedKeeper, wasmViewKeeper *wasmkeeper.Keeper) *Keeper {
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
		transferKeeper:               transferKeeper,
		wasmKeeper:                   wasmKeeper,
		wasmViewKeeper:               wasmViewKeeper,
		pendingTxs:                   make(map[string][]*PendingTx),
		nonceMx:                      &sync.RWMutex{},
		cachedFeeCollectorAddressMtx: &sync.RWMutex{},
		keyToNonce:                   make(map[tmtypes.TxKey]*AddressNoncePair),
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

func (k *Keeper) WasmKeeper() *wasmkeeper.PermissionedKeeper {
	return k.wasmKeeper
}

func (k *Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}

func (k *Keeper) IterateAll(ctx sdk.Context, pref []byte, cb func(key, val []byte) bool) {
	iter := k.PrefixStore(ctx, pref).Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		if cb(iter.Key(), iter.Value()) {
			break
		}
	}
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
	if k.EthBlockTestConfig.Enabled {
		return k.getBlockTestBlockCtx(ctx)
	}
	if k.EthReplayConfig.Enabled {
		return k.getReplayBlockCtx(ctx)
	}
	coinbase, err := k.GetFeeCollectorAddress(ctx)
	if err != nil {
		return nil, err
	}
	r, err := ctx.BlockHeader().Time.MarshalBinary()
	if err != nil {
		return nil, err
	}
	rh := common.BytesToHash(r)

	txfer := func(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
		if IsPayablePrecompile(&recipient) {
			state.TransferWithoutEvents(db, sender, recipient, amount)
		} else {
			core.Transfer(db, sender, recipient, amount)
		}
	}

	return &vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    txfer,
		GetHash:     k.GetHashFn(ctx),
		Coinbase:    coinbase,
		GasLimit:    gp.Gas(),
		BlockNumber: big.NewInt(ctx.BlockHeight()),
		Time:        uint64(ctx.BlockHeader().Time.Unix()),
		Difficulty:  utils.Big0,                                     // only needed for PoW
		BaseFee:     k.GetBaseFeePerGas(ctx).TruncateInt().BigInt(), // feemarket not enabled
		BlobBaseFee: utils.Big0,                                     // Cancun not enabled
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
		if k.txResults[txIdx].Code == 0 || value.(*EvmTxDeferredInfo).Error != "" {
			res = append(res, *(value.(*EvmTxDeferredInfo)))
		}
		return true
	})
	sort.SliceStable(res, func(i, j int) bool { return res[i].TxIndx < res[j].TxIndx })
	return
}

func (k *Keeper) AppendToEvmTxDeferredInfo(ctx sdk.Context, bloom ethtypes.Bloom, txHash common.Hash, surplus sdk.Int) {
	k.deferredInfo.Store(ctx.TxIndex(), &EvmTxDeferredInfo{
		TxIndx:  ctx.TxIndex(),
		TxBloom: bloom,
		TxHash:  txHash,
		Surplus: surplus,
	})
}

func (k *Keeper) AppendErrorToEvmTxDeferredInfo(ctx sdk.Context, txHash common.Hash, err string) {
	k.deferredInfo.Store(ctx.TxIndex(), &EvmTxDeferredInfo{
		TxIndx: ctx.TxIndex(),
		TxHash: txHash,
		Error:  err,
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
	pending := k.pendingTxs[addr.Hex()]

	// Check each nonce starting from latest until we find a gap
	// That gap is the next nonce we should use.
	for ; ; nextNonce++ {
		// if it's not in pending, then it's the next nonce
		if _, found := sort.Find(len(pending), func(i int) int { return uint64Cmp(nextNonce, pending[i].Nonce) }); !found {
			return nextNonce
		}
	}
}

// AddPendingNonce adds a pending nonce to the keeper
func (k *Keeper) AddPendingNonce(key tmtypes.TxKey, addr common.Address, nonce uint64, priority int64) {
	k.nonceMx.Lock()
	defer k.nonceMx.Unlock()

	addrStr := addr.Hex()
	if existing, ok := k.keyToNonce[key]; ok {
		if existing.Nonce != nonce {
			fmt.Printf("Seeing transactions with the same hash %X but different nonces (%d vs. %d), which should be impossible\n", key, nonce, existing.Nonce)
		}
		if existing.Address != addr {
			fmt.Printf("Seeing transactions with the same hash %X but different addresses (%s vs. %s), which should be impossible\n", key, addr.Hex(), existing.Address.Hex())
		}
		// we want to no-op whether it's a genuine duplicate or not
		return
	}
	for _, pendingTx := range k.pendingTxs[addrStr] {
		if pendingTx.Nonce == nonce {
			if priority > pendingTx.Priority {
				// replace existing tx
				delete(k.keyToNonce, pendingTx.Key)
				pendingTx.Priority = priority
				pendingTx.Key = key
				k.keyToNonce[key] = &AddressNoncePair{
					Address: addr,
					Nonce:   nonce,
				}
			}
			// we don't need to return error here if priority is lower.
			// Tendermint will take care of rejecting the tx from mempool
			return
		}
	}
	k.keyToNonce[key] = &AddressNoncePair{
		Address: addr,
		Nonce:   nonce,
	}
	k.pendingTxs[addrStr] = append(k.pendingTxs[addrStr], &PendingTx{
		Key:      key,
		Nonce:    nonce,
		Priority: priority,
	})
	slices.SortStableFunc(k.pendingTxs[addrStr], func(a, b *PendingTx) int {
		if a.Nonce < b.Nonce {
			return -1
		} else if a.Nonce > b.Nonce {
			return 1
		}
		return 0
	})
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

	addr := tx.Address.Hex()
	pendings := k.pendingTxs[addr]
	firstMatch, found := sort.Find(len(pendings), func(i int) int { return uint64Cmp(tx.Nonce, pendings[i].Nonce) })
	if !found {
		fmt.Printf("Removing tx %X without a corresponding pending nonce, which should not happen\n", key)
		return
	}
	k.pendingTxs[addr] = append(k.pendingTxs[addr][:firstMatch], k.pendingTxs[addr][firstMatch+1:]...)
	if len(k.pendingTxs[addr]) == 0 {
		delete(k.pendingTxs, addr)
	}
}

func (k *Keeper) SetTxResults(txResults []*abci.ExecTxResult) {
	k.txResults = txResults
}

// Test use only
func (k *Keeper) GetPendingTxs() map[string][]*PendingTx {
	return k.pendingTxs
}

// Test use only
func (k *Keeper) GetKeysToNonces() map[tmtypes.TxKey]*AddressNoncePair {
	return k.keyToNonce
}

// Only used in ETH replay
func (k *Keeper) PrepareReplayedAddr(ctx sdk.Context, addr common.Address) {
	if !k.EthReplayConfig.Enabled {
		return
	}
	store := k.PrefixStore(ctx, types.ReplaySeenAddrPrefix)
	bz := store.Get(addr[:])
	if len(bz) > 0 {
		return
	}
	a, err := k.Trie.GetAccount(addr)
	if err != nil || a == nil {
		return
	}
	store.Set(addr[:], a.Root[:])
	if a.Balance != nil && a.Balance.Cmp(utils.Big0) != 0 {
		usei, wei := state.SplitUseiWeiAmount(a.Balance)
		err = k.BankKeeper().AddCoins(ctx, k.GetSeiAddressOrDefault(ctx, addr), sdk.NewCoins(sdk.NewCoin("usei", usei)), true)
		if err != nil {
			panic(err)
		}
		err = k.BankKeeper().AddWei(ctx, k.GetSeiAddressOrDefault(ctx, addr), wei)
		if err != nil {
			panic(err)
		}
	}
	k.SetNonce(ctx, addr, a.Nonce)
	if !bytes.Equal(a.CodeHash, ethtypes.EmptyCodeHash.Bytes()) {
		k.PrefixStore(ctx, types.CodeHashKeyPrefix).Set(addr[:], a.CodeHash)
		code, err := k.DB.ContractCode(addr, common.BytesToHash(a.CodeHash))
		if err != nil {
			panic(err)
		}
		if len(code) > 0 {
			k.PrefixStore(ctx, types.CodeKeyPrefix).Set(addr[:], code)
			length := make([]byte, 8)
			binary.BigEndian.PutUint64(length, uint64(len(code)))
			k.PrefixStore(ctx, types.CodeSizeKeyPrefix).Set(addr[:], length)
		}
	}
}

func (k *Keeper) GetBaseFee(ctx sdk.Context) *big.Int {
	if k.EthReplayConfig.Enabled {
		return k.ReplayBlock.Header_.BaseFee
	}
	if k.EthBlockTestConfig.Enabled {
		bb := k.BlockTest.Json.Blocks[ctx.BlockHeight()-1]
		b, err := bb.Decode()
		if err != nil {
			panic(err)
		}
		return b.Header_.BaseFee
	}
	return nil
}

func (k *Keeper) GetReplayedHeight(ctx sdk.Context) int64 {
	return k.getInt64State(ctx, types.ReplayedHeight)
}

func (k *Keeper) SetReplayedHeight(ctx sdk.Context) {
	k.setInt64State(ctx, types.ReplayedHeight, ctx.BlockHeight())
}

func (k *Keeper) GetReplayInitialHeight(ctx sdk.Context) int64 {
	return k.getInt64State(ctx, types.ReplayInitialHeight)
}

func (k *Keeper) SetReplayInitialHeight(ctx sdk.Context, h int64) {
	k.setInt64State(ctx, types.ReplayInitialHeight, h)
}

func (k *Keeper) setInt64State(ctx sdk.Context, key []byte, val int64) {
	store := ctx.KVStore(k.storeKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(val))
	store.Set(key, bz)
}

func (k *Keeper) getInt64State(ctx sdk.Context, key []byte) int64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(key)
	if bz == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(bz))
}

func (k *Keeper) getBlockTestBlockCtx(ctx sdk.Context) (*vm.BlockContext, error) {
	bb := k.BlockTest.Json.Blocks[ctx.BlockHeight()-1]
	b, err := bb.Decode()
	if err != nil {
		return nil, err
	}
	header := b.Header_
	getHash := func(height uint64) common.Hash {
		height = height + 1
		for i := 0; i < len(k.BlockTest.Json.Blocks); i++ {
			if k.BlockTest.Json.Blocks[i].BlockHeader.Number.Uint64() == height {
				return k.BlockTest.Json.Blocks[i].BlockHeader.Hash
			}
		}
		panic(fmt.Sprintf("block hash not found for height %d", height))
	}
	var (
		baseFee     *big.Int
		blobBaseFee *big.Int
		random      *common.Hash
	)
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	if header.ExcessBlobGas != nil {
		blobBaseFee = eip4844.CalcBlobFee(*header.ExcessBlobGas)
	} else {
		blobBaseFee = eip4844.CalcBlobFee(0)
	}
	if header.Difficulty.Cmp(common.Big0) == 0 {
		random = &header.MixDigest
	}
	return &vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     getHash,
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        header.Time,
		Difficulty:  new(big.Int).Set(header.Difficulty),
		BaseFee:     baseFee,
		BlobBaseFee: blobBaseFee,
		Random:      random,
	}, nil
}

func (k *Keeper) getReplayBlockCtx(ctx sdk.Context) (*vm.BlockContext, error) {
	header := k.ReplayBlock.Header_
	getHash := core.GetHashFn(header, &ReplayChainContext{ethClient: k.EthClient})
	var (
		baseFee     *big.Int
		blobBaseFee *big.Int
		random      *common.Hash
	)
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	if header.ExcessBlobGas != nil {
		blobBaseFee = eip4844.CalcBlobFee(*header.ExcessBlobGas)
	}
	if header.Difficulty.Cmp(common.Big0) == 0 {
		random = &header.MixDigest
	}
	return &vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     getHash,
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        header.Time,
		Difficulty:  new(big.Int).Set(header.Difficulty),
		BaseFee:     baseFee,
		BlobBaseFee: blobBaseFee,
		Random:      random,
	}, nil
}

func uint64Cmp(a, b uint64) int {
	if a < b {
		return -1
	} else if a == b {
		return 0
	}
	return 1
}
