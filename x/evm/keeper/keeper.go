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
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	ibctransferkeeper "github.com/cosmos/ibc-go/v3/modules/apps/transfer/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/holiman/uint256"
	seidbtypes "github.com/sei-protocol/sei-db/ss/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"

	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/blocktest"
	"github.com/sei-protocol/sei-chain/x/evm/querier"
	"github.com/sei-protocol/sei-chain/x/evm/replay"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const Pacific1ChainID = "pacific-1"

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
	CachingDB   *ethstate.CachingDB
	Root        common.Hash
	ReplayBlock *ethtypes.Block

	receiptStore seidbtypes.StateStore

	customPrecompiles       map[common.Address]putils.VersionedPrecompiles
	latestCustomPrecompiles map[common.Address]vm.PrecompiledContract
	latestUpgrade           string
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

func (ctx *ReplayChainContext) Config() *params.ChainConfig {
	return types.DefaultChainConfig().EthereumConfig(ctx.chainID)
}

func NewKeeper(
	storeKey sdk.StoreKey, transientStoreKey sdk.StoreKey, paramstore paramtypes.Subspace, receiptStateStore seidbtypes.StateStore,
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
		GasLimit:    gp.Gas(),
		BlockNumber: big.NewInt(ctx.BlockHeight()),
		Time:        uint64(ctx.BlockHeader().Time.Unix()),
		Difficulty:  utils.Big0, // only needed for PoW
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
	if a.Balance != nil && a.Balance.CmpBig(utils.Big0) != 0 {
		usei, wei := state.SplitUseiWeiAmount(a.Balance.ToBig())
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
		code := k.CachingDB.ContractCodeWithPrefix(addr, common.BytesToHash(a.CodeHash))
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
	if ctx.ChainID() == Pacific1ChainID && ctx.BlockHeight() < k.upgradeKeeper.GetDoneHeight(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)), "6.2.0") {
		return nil
	}
	return k.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
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
	chainConfig := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	blobBaseFee = eip4844.CalcBlobFee(chainConfig, header)
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
	replayCtx := &ReplayChainContext{ethClient: k.EthClient, chainID: k.ChainID(ctx)}
	getHash := core.GetHashFn(header, replayCtx)
	var (
		baseFee     *big.Int
		blobBaseFee *big.Int
		random      *common.Hash
	)
	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	} else {
		baseFee = big.NewInt(0)
	}
	if header.ExcessBlobGas != nil {
		blobBaseFee = eip4844.CalcBlobFee(replayCtx.Config(), header)
	} else {
		blobBaseFee = big.NewInt(0)
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
