package keeper

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"sync"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	bankkeeper "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	upgradekeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	ibctransferkeeper "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/keeper"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	wasmkeeper "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/keeper"
	"github.com/sei-protocol/sei-chain/utils"
)

const Pacific1ChainID = "pacific-1"
const DefaultBlockGasLimit = 10000000

type Keeper struct {
	storeKey          sdk.StoreKey
	transientStoreKey sdk.StoreKey

	Paramstore paramtypes.Subspace

	txResults []*abci.ExecTxResult
	msgs      []*types.MsgEVMTransaction

	bankKeeper     bankkeeper.Keeper
	accountKeeper  *authkeeper.AccountKeeper
	stakingKeeper  *stakingkeeper.Keeper
	transferKeeper ibctransferkeeper.Keeper
	wasmKeeper     *wasmkeeper.PermissionedKeeper
	wasmViewKeeper *wasmkeeper.Keeper
	upgradeKeeper  *upgradekeeper.Keeper

	cachedFeeCollectorAddressMtx *sync.RWMutex
	cachedFeeCollectorAddress    *common.Address
	nonceMx                      *sync.RWMutex
	pendingTxs                   map[string][]*PendingTx
	keyToNonce                   map[tmtypes.TxKey]*AddressNoncePair

	// used for both ETH replay and block tests. Not used in chain critical path.
	Trie        ethstate.Trie
	DB          ethstate.Database
	CachingDB   *ethstate.CachingDB
	Root        common.Hash
	ReplayBlock *ethtypes.Block

	receiptStore receipt.ReceiptStore

	customPrecompiles       map[common.Address]putils.VersionedPrecompiles
	latestCustomPrecompiles map[common.Address]vm.PrecompiledContract
	latestUpgrade           string

	// EvmoneVM holds the loaded evmone VM instance for the Giga executor
	EvmoneVM *evmc.VM

	// UseRegularStore when true causes PrefixStore to use ctx.KVStore instead of ctx.GigaKVStore.
	// This is for debugging/testing to isolate Giga executor logic from GigaKVStore layer.
	UseRegularStore bool
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
	chainID   *big.Int
	params    types.Params
}

func (ctx *ReplayChainContext) Engine() consensus.Engine {
	return nil
}

func (ctx *ReplayChainContext) GetHeader(hash common.Hash, number uint64) *ethtypes.Header {
	res, err := ctx.ethClient.BlockByNumber(context.Background(), big.NewInt(int64(number))) //nolint:gosec
	if err != nil || res.Header_.Hash() != hash {
		return nil
	}
	return res.Header_
}

func (ctx *ReplayChainContext) Config() *params.ChainConfig {
	return types.DefaultChainConfig().EthereumConfig(ctx.chainID)
}

func NewKeeper(
	storeKey sdk.StoreKey, transientStoreKey sdk.StoreKey, paramstore paramtypes.Subspace, receiptStateStore receipt.ReceiptStore,
	bankKeeper bankkeeper.Keeper, accountKeeper *authkeeper.AccountKeeper, stakingKeeper *stakingkeeper.Keeper,
	transferKeeper ibctransferkeeper.Keeper, wasmKeeper *wasmkeeper.PermissionedKeeper, wasmViewKeeper *wasmkeeper.Keeper, upgradeKeeper *upgradekeeper.Keeper) *Keeper {

	if !paramstore.HasKeyTable() {
		paramstore = paramstore.WithKeyTable(types.ParamKeyTable())
	}
	k := &Keeper{
		storeKey:                     storeKey,
		transientStoreKey:            transientStoreKey,
		Paramstore:                   paramstore,
		bankKeeper:                   bankKeeper,
		accountKeeper:                accountKeeper,
		stakingKeeper:                stakingKeeper,
		transferKeeper:               transferKeeper,
		wasmKeeper:                   wasmKeeper,
		wasmViewKeeper:               wasmViewKeeper,
		upgradeKeeper:                upgradeKeeper,
		pendingTxs:                   make(map[string][]*PendingTx),
		nonceMx:                      &sync.RWMutex{},
		cachedFeeCollectorAddressMtx: &sync.RWMutex{},
		keyToNonce:                   make(map[tmtypes.TxKey]*AddressNoncePair),
		receiptStore:                 receiptStateStore,
	}
	return k
}

func (k *Keeper) SetCustomPrecompiles(cp map[common.Address]putils.VersionedPrecompiles, latestUpgrade string) {
	k.customPrecompiles = cp
	k.latestUpgrade = latestUpgrade
	k.latestCustomPrecompiles = make(map[common.Address]vm.PrecompiledContract, len(cp))
	for addr, versioned := range cp {
		k.latestCustomPrecompiles[addr] = versioned[latestUpgrade]
	}
}

func (k *Keeper) CustomPrecompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract {
	if !ctx.IsTracing() {
		return k.latestCustomPrecompiles
	}
	versions := k.GetCustomPrecompilesVersions(ctx)
	cp := make(map[common.Address]vm.PrecompiledContract, len(k.customPrecompiles))
	for addr, versioned := range k.customPrecompiles {
		cp[addr] = versioned[versions[addr]]
	}
	return cp
}

func (k *Keeper) GetCustomPrecompilesVersions(ctx sdk.Context) map[common.Address]string {
	height := ctx.BlockHeight()
	cp := make(map[common.Address]string, len(k.customPrecompiles))
	for addr, versioned := range k.customPrecompiles {
		mostRecentUpgradeHeight := int64(0)
		noForkHistory := true
		for upgrade := range versioned {
			upgradeHeight := k.upgradeKeeper.GetDoneHeight(ctx, upgrade)
			if upgradeHeight != 0 {
				noForkHistory = false
			}
			if height < upgradeHeight {
				// requested height hasn't seen this upgrade version yet.
				continue
			}
			if upgradeHeight > mostRecentUpgradeHeight {
				mostRecentUpgradeHeight = upgradeHeight
				cp[addr] = upgrade
			}
		}
		if noForkHistory {
			cp[addr] = k.latestUpgrade
		}
	}
	return cp
}

func (k *Keeper) AccountKeeper() *authkeeper.AccountKeeper {
	return k.accountKeeper
}

func (k *Keeper) BankKeeper() bankkeeper.Keeper {
	return k.bankKeeper
}

func (k *Keeper) ReceiptStore() receipt.ReceiptStore {
	return k.receiptStore
}

func (k *Keeper) WasmKeeper() *wasmkeeper.PermissionedKeeper {
	return k.wasmKeeper
}

func (k *Keeper) UpgradeKeeper() *upgradekeeper.Keeper {
	return k.upgradeKeeper
}

func (k *Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}

func (k *Keeper) IterateAll(ctx sdk.Context, pref []byte, cb func(key, val []byte) bool) {
	iter := k.PrefixStore(ctx, pref).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		if cb(iter.Key(), iter.Value()) {
			break
		}
	}
}

// GetKVStore returns the appropriate KVStore based on the UseRegularStore flag.
// When UseRegularStore is true (for debugging/testing), returns regular KVStore.
// Otherwise returns GigaKVStore.
func (k *Keeper) GetKVStore(ctx sdk.Context) sdk.KVStore {
	if k.UseRegularStore {
		return ctx.KVStore(k.GetStoreKey())
	}
	return ctx.GigaKVStore(k.GetStoreKey())
}

func (k *Keeper) PrefixStore(ctx sdk.Context, pref []byte) sdk.KVStore {
	return prefix.NewStore(k.GetKVStore(ctx), pref)
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

	// Use hash of block timestamp as info for PREVRANDAO
	r, err := ctx.BlockHeader().Time.MarshalBinary()
	if err != nil {
		return nil, err
	}
	rh := crypto.Keccak256Hash(r)

	txfer := func(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
		if IsPayablePrecompile(&recipient) {
			state.TransferWithoutEvents(db, sender, recipient, amount)
		} else {
			core.Transfer(db, sender, recipient, amount)
		}
	}
	var baseFee *big.Int
	if ctx.ChainID() == Pacific1ChainID && ctx.BlockHeight() < 114945913 {
		baseFee = k.GetBaseFeePerGas(ctx).TruncateInt().BigInt()
	} else {
		baseFee = k.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
	}

	return &vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    txfer,
		GetHash:     k.GetHashFn(ctx),
		Coinbase:    coinbase,
		GasLimit: func() uint64 {
			if ctx.ConsensusParams() != nil && ctx.ConsensusParams().Block != nil {
				return uint64(ctx.ConsensusParams().Block.MaxGas) //nolint:gosec
			}
			return DefaultBlockGasLimit
		}(),
		BlockNumber: big.NewInt(ctx.BlockHeight()),
		Time:        uint64(ctx.BlockHeader().Time.Unix()), //nolint:gosec
		Difficulty:  utils.Big0,                            // only needed for PoW
		BaseFee:     baseFee,
		BlobBaseFee: utils.Big1, // Cancun not enabled
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

func (k *Keeper) SetMsgs(msgs []*types.MsgEVMTransaction) {
	k.msgs = msgs
}

// Test use only
func (k *Keeper) GetPendingTxs() map[string][]*PendingTx {
	return k.pendingTxs
}

// Test use only
func (k *Keeper) GetKeysToNonces() map[tmtypes.TxKey]*AddressNoncePair {
	return k.keyToNonce
}

func (k *Keeper) GetBaseFee(ctx sdk.Context) *big.Int {
	if ctx.ChainID() == Pacific1ChainID && ctx.BlockHeight() < k.upgradeKeeper.GetDoneHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), "6.2.0") {
		return nil
	}
	return k.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
}

func (k *Keeper) setInt64State(ctx sdk.Context, key []byte, val int64) {
	store := k.GetKVStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(val)) //nolint:gosec
	store.Set(key, bz)
}

func (k *Keeper) getInt64State(ctx sdk.Context, key []byte) int64 {
	store := k.GetKVStore(ctx)
	bz := store.Get(key)
	if bz == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(bz)) //nolint:gosec
}

func (k *Keeper) GetGasPool() core.GasPool {
	return math.MaxUint64
}

func (k *Keeper) ShouldUseRegularStore() bool {
	return k.UseRegularStore
}

func uint64Cmp(a, b uint64) int {
	if a < b {
		return -1
	} else if a == b {
		return 0
	}
	return 1
}
