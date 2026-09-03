package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"runtime"
	"slices"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const evmOnlyInMemoryMinGasPrice = 1_000_000_000

var evmOnlyInMemoryBaseBalance = new(big.Int).Lsh(big.NewInt(1), 200)

type evmOnlyInMemoryApplication struct {
	abci.BaseApplication

	chainID     *big.Int
	chainConfig *params.ChainConfig
	store       *evmonly.MemoryStore
	validators  []abci.ValidatorUpdate
	preparedTxs *evmOnlyPreparedTxCache
	executor    utils.AtomicSend[utils.Option[*evmonly.Executor]]
	gasLimit    atomic.Uint64
	state       utils.Mutex[*evmOnlyInMemoryState]
}

type evmOnlyInMemoryState struct {
	nextHeight      int64
	committedHeight int64
	appHash         common.Hash
	parentHash      common.Hash
	pending         utils.Option[evmOnlyInMemoryPending]
}

type evmOnlyInMemoryPending struct {
	height    int64
	appHash   common.Hash
	blockHash common.Hash
}

type evmOnlyInMemoryPreparedBlock struct {
	app       *evmOnlyInMemoryApplication
	height    int64
	number    uint64
	blockHash common.Hash
	block     evmonly.PreparedBlock
}

var _ abci.Application = (*evmOnlyInMemoryApplication)(nil)
var _ abci.BlockPreparingApplication = (*evmOnlyInMemoryApplication)(nil)
var _ abci.PreparedBlock = (*evmOnlyInMemoryPreparedBlock)(nil)

// NewEVMOnlyInMemoryApplication returns an ephemeral raw-Ethereum application for
// Autobahn Docker load tests.
func NewEVMOnlyInMemoryApplication(chainID uint64, validators []abci.ValidatorUpdate) abci.Application {
	base := evmOnlyFundedState{}
	store := evmonly.NewMemoryStore(base)
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	return &evmOnlyInMemoryApplication{
		chainID:     new(big.Int).SetUint64(chainID),
		chainConfig: &chainConfig,
		store:       store,
		validators:  slices.Clone(validators),
		preparedTxs: newEVMOnlyPreparedTxCache(),
		executor:    utils.NewAtomicSend(utils.None[*evmonly.Executor]()),
		state:       utils.NewMutex(&evmOnlyInMemoryState{}),
	}
}

func (a *evmOnlyInMemoryApplication) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("EVM-only initial height must be positive: %d", req.InitialHeight)
	}
	gasLimit, err := evmOnlyInMemoryGasLimit(req)
	if err != nil {
		return nil, err
	}
	for state := range a.state.Lock() {
		if a.executor.Load().IsPresent() {
			return nil, fmt.Errorf("EVM-only application already initialized")
		}
		executor := evmonly.NewExecutor(evmonly.Config{
			ChainConfig:         a.chainConfig,
			MinGasPrice:         big.NewInt(evmOnlyInMemoryMinGasPrice),
			OCCWorkers:          runtime.GOMAXPROCS(0),
			ParseWorkers:        runtime.GOMAXPROCS(0),
			BlockResultPoolSize: 1,
		}, evmonly.WithStore(a.store, a.store.EncodeChangeSet))
		a.gasLimit.Store(gasLimit)
		a.executor.Store(utils.Some(executor))
		state.nextHeight = req.InitialHeight
		state.committedHeight = req.InitialHeight - 1
		return &abci.ResponseInitChain{}, nil
	}
	panic("unreachable")
}

func evmOnlyInMemoryGasLimit(req *abci.RequestInitChain) (uint64, error) {
	if req.ConsensusParams == nil || req.ConsensusParams.Block == nil || req.ConsensusParams.Block.MaxGas <= 0 {
		return 0, fmt.Errorf("EVM-only max gas must be positive")
	}
	gasLimit, ok := utils.SafeCast[uint64](req.ConsensusParams.Block.MaxGas)
	if !ok {
		return 0, fmt.Errorf("EVM-only max gas exceeds uint64: %d", req.ConsensusParams.Block.MaxGas)
	}
	return gasLimit, nil
}

func (a *evmOnlyInMemoryApplication) Info() *abci.ResponseInfo {
	for state := range a.state.Lock() {
		return &abci.ResponseInfo{
			Data:             "evmonly-in-memory",
			LastBlockHeight:  state.committedHeight,
			LastBlockAppHash: append([]byte(nil), state.appHash[:]...),
		}
	}
	panic("unreachable")
}

func (a *evmOnlyInMemoryApplication) LastBlockHeight() int64 {
	for state := range a.state.Lock() {
		return state.committedHeight
	}
	panic("unreachable")
}

func (a *evmOnlyInMemoryApplication) GetValidators() []abci.ValidatorUpdate {
	return slices.Clone(a.validators)
}

func (a *evmOnlyInMemoryApplication) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	// TODO(evmonly-production): close the gap between admission and block validity
	// before accepting arbitrary traffic; this test app assumes executable load-test transactions.
	tx, sender, err := a.parseTx(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: err.Error()}}
	}
	gasWanted, ok := utils.SafeCast[int64](tx.Gas())
	if !ok {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: "transaction gas limit exceeds int64"}}
	}
	hash := tx.Hash()
	a.preparedTxs.Put(hash, evmonly.PreparedTx{Tx: tx, Sender: sender})
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    gasWanted,
			GasEstimated: gasWanted,
		},
		IsEVM:            true,
		EVMNonce:         tx.Nonce(),
		EVMHash:          hash,
		EVMSenderAddress: sender,
		SeiSenderAddress: append([]byte(nil), sender[:]...),
	}
}

func (a *evmOnlyInMemoryApplication) parseTx(raw []byte) (*ethtypes.Transaction, common.Address, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, common.Address{}, err
	}
	if !tx.Protected() {
		return nil, common.Address{}, fmt.Errorf("unprotected Ethereum transaction")
	}
	if tx.ChainId().Cmp(a.chainID) != 0 {
		return nil, common.Address{}, fmt.Errorf("ethereum transaction chain ID does not match %s", a.chainID)
	}
	if tx.Type() == ethtypes.BlobTxType {
		return nil, common.Address{}, fmt.Errorf("blob transactions are not supported")
	}
	if tx.GasPrice().Cmp(big.NewInt(evmOnlyInMemoryMinGasPrice)) < 0 {
		return nil, common.Address{}, fmt.Errorf("ethereum transaction gas price is below %d", evmOnlyInMemoryMinGasPrice)
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(a.chainID), tx)
	if err != nil {
		return nil, common.Address{}, err
	}
	return tx, sender, nil
}

func evmOnlyStoreAddress(address common.Address) gigastore.Address {
	var storeAddress gigastore.Address
	copy(storeAddress[:], address[:])
	return storeAddress
}

func (a *evmOnlyInMemoryApplication) EvmNonce(address common.Address) uint64 {
	snapshot := a.store.OpenView()
	defer snapshot.Close()
	return snapshot.GetNonce(evmOnlyStoreAddress(address))
}

func (a *evmOnlyInMemoryApplication) EvmBalance(address common.Address, _ []byte) uint256.Int {
	snapshot := a.store.OpenView()
	defer snapshot.Close()
	balance := snapshot.GetBalance(evmOnlyStoreAddress(address))
	return *new(uint256.Int).SetBytes(balance[:])
}

func (a *evmOnlyInMemoryApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	var parentHash common.Hash
	for state := range a.state.Lock() {
		parentHash = state.parentHash
	}
	prepared, err := a.prepareBlock(ctx, req, parentHash)
	if err != nil {
		return nil, err
	}
	return prepared.Finalize(ctx)
}

// PrepareBlock decodes transactions and recovers their senders before ordered finalization.
func (a *evmOnlyInMemoryApplication) PrepareBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (abci.PreparedBlock, error) {
	return a.prepareBlock(ctx, req, common.BytesToHash(req.Header.LastBlockId.Hash))
}

func (a *evmOnlyInMemoryApplication) prepareBlock(
	ctx context.Context,
	req *abci.RequestFinalizeBlock,
	parentHash common.Hash,
) (*evmOnlyInMemoryPreparedBlock, error) {
	height := req.Header.Height
	if height <= 0 {
		return nil, fmt.Errorf("EVM-only block height must be positive: %d", height)
	}
	number, ok := utils.SafeCast[uint64](height)
	if !ok {
		return nil, fmt.Errorf("EVM-only block height exceeds uint64: %d", height)
	}
	timestamp, ok := utils.SafeCast[uint64](req.Header.Time.Unix())
	if !ok {
		return nil, fmt.Errorf("EVM-only block timestamp is negative: %s", req.Header.Time)
	}
	blockHash := common.BytesToHash(req.Hash)
	executor, ok := a.executor.Load().Get()
	if !ok {
		return nil, fmt.Errorf("EVM-only block prepared before InitChain")
	}
	prepared, err := executor.PrepareBlockWithLookup(ctx, evmonly.BlockRequest{
		Context: evmonly.BlockContext{
			Number:      number,
			Time:        timestamp,
			GasLimit:    a.gasLimit.Load(),
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     new(big.Int),
			BlobBaseFee: new(big.Int),
			ParentHash:  parentHash,
			BlockHash:   blockHash,
			PrevRandao:  crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, timestamp)),
		},
		Txs: req.Txs,
	}, a.preparedTxs.Lookup)
	if err != nil {
		return nil, err
	}
	return &evmOnlyInMemoryPreparedBlock{
		app:       a,
		height:    height,
		number:    number,
		blockHash: blockHash,
		block:     prepared,
	}, nil
}

// Finalize applies the prepared transactions to the application state.
func (b *evmOnlyInMemoryPreparedBlock) Finalize(ctx context.Context) (*abci.ResponseFinalizeBlock, error) {
	return b.app.finalizePreparedBlock(ctx, b)
}

func (a *evmOnlyInMemoryApplication) finalizePreparedBlock(
	ctx context.Context,
	prepared *evmOnlyInMemoryPreparedBlock,
) (*abci.ResponseFinalizeBlock, error) {
	for state := range a.state.Lock() {
		executor, ok := a.executor.Load().Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		if state.pending.IsPresent() {
			return nil, fmt.Errorf("EVM-only block %d finalized before committing the previous block", prepared.height)
		}
		if prepared.height != state.nextHeight {
			return nil, fmt.Errorf("EVM-only block height %d does not match next height %d", prepared.height, state.nextHeight)
		}
		if prepared.block.Context.ParentHash != state.parentHash {
			return nil, fmt.Errorf("EVM-only block %d parent hash does not match committed parent", prepared.height)
		}
		result, err := executor.ExecutePreparedBlock(ctx, prepared.block)
		if err != nil {
			return nil, err
		}
		defer result.Release()
		appHash := hashEVMOnlyInMemoryResult(state.appHash, prepared.number, prepared.blockHash, result)
		state.pending = utils.Some(evmOnlyInMemoryPending{
			height:    prepared.height,
			appHash:   appHash,
			blockHash: prepared.blockHash,
		})
		return &abci.ResponseFinalizeBlock{
			AppHash:   append([]byte(nil), appHash[:]...),
			TxResults: evmOnlyABCIResults(result),
		}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyInMemoryApplication) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.state.Lock() {
		pending, ok := state.pending.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only Commit called without a finalized block")
		}
		state.committedHeight = pending.height
		state.nextHeight = pending.height + 1
		state.appHash = pending.appHash
		state.parentHash = pending.blockHash
		state.pending = utils.None[evmOnlyInMemoryPending]()
		return &abci.ResponseCommit{}, nil
	}
	panic("unreachable")
}

func evmOnlyABCIResults(result *evmonly.BlockResult) []*abci.ExecTxResult {
	txResults := make([]*abci.ExecTxResult, len(result.Txs))
	values := make([]abci.ExecTxResult, len(result.Txs))
	for i, tx := range result.Txs {
		gasUsed := utils.Clamp[int64](tx.GasUsed)
		values[i] = abci.ExecTxResult{
			Code:      abci.CodeTypeOK,
			GasWanted: gasUsed,
			GasUsed:   gasUsed,
		}
		txResults[i] = &values[i]
	}
	return txResults
}

func hashEVMOnlyInMemoryResult(previous common.Hash, height uint64, blockHash common.Hash, result *evmonly.BlockResult) common.Hash {
	h := sha256.New()
	_, _ = h.Write(previous[:])
	writeEVMOnlyHashUint64(h, height)
	_, _ = h.Write(blockHash[:])
	writeEVMOnlyHashUint64(h, result.GasUsed)

	changes := result.ChangeSet
	writeEVMOnlyHashSection(h, 1, len(changes.Balances))
	for _, change := range changes.Balances {
		_, _ = h.Write(change.Address[:])
		var balance [common.HashLength]byte
		if change.Balance != nil {
			change.Balance.FillBytes(balance[:])
		}
		_, _ = h.Write(balance[:])
	}
	writeEVMOnlyHashSection(h, 2, len(changes.Nonces))
	for _, change := range changes.Nonces {
		_, _ = h.Write(change.Address[:])
		writeEVMOnlyHashUint64(h, change.Nonce)
	}
	writeEVMOnlyHashSection(h, 3, len(changes.Code))
	for _, change := range changes.Code {
		_, _ = h.Write(change.Address[:])
		writeEVMOnlyHashBool(h, change.Delete)
		writeEVMOnlyHashBytes(h, change.Code)
	}
	writeEVMOnlyHashSection(h, 4, len(changes.StorageClears))
	for _, address := range changes.StorageClears {
		_, _ = h.Write(address[:])
	}
	writeEVMOnlyHashSection(h, 5, len(changes.Storage))
	for _, change := range changes.Storage {
		_, _ = h.Write(change.Address[:])
		_, _ = h.Write(change.Key[:])
		writeEVMOnlyHashBool(h, change.Delete)
		_, _ = h.Write(change.Value[:])
	}
	return common.BytesToHash(h.Sum(nil))
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeEVMOnlyHashBytes(w byteWriter, value []byte) {
	writeEVMOnlyHashUint64(w, uint64(len(value)))
	_, _ = w.Write(value)
}

func writeEVMOnlyHashSection(w byteWriter, kind byte, count int) {
	_, _ = w.Write([]byte{kind})
	writeEVMOnlyHashUint64(w, utils.Clamp[uint64](count))
}

func writeEVMOnlyHashBool(w byteWriter, value bool) {
	if value {
		_, _ = w.Write([]byte{1})
		return
	}
	_, _ = w.Write([]byte{0})
}

func writeEVMOnlyHashUint64(w byteWriter, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = w.Write(encoded[:])
}

type evmOnlyFundedState struct{}

func (evmOnlyFundedState) AccountExists(common.Address) bool { return true }
func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyInMemoryBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
