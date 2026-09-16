package evmonlyapp

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"runtime"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const evmOnlyMinGasPrice = 1_000_000_000

// evmOnlyBaseFee is the base fee this application executes every block at.
// Admission and block validity both price against it, so they cannot diverge.
func evmOnlyBaseFee() *big.Int { return new(big.Int) }

var evmOnlyBaseBalance = new(big.Int).Lsh(big.NewInt(1), 200)

type evmOnlyApplication struct {
	abci.BaseApplication

	chainID          *big.Int
	chainConfig      *params.ChainConfig
	storage          *bootstrap.GigaStorageManager
	changeSetEncoder evmonly.NamedChangeSetEncoder
	validators       []abci.ValidatorUpdate
	state            utils.Mutex[*evmOnlyState]
}

type evmOnlyState struct {
	executor        utils.Option[*evmonly.Executor]
	gasLimit        uint64
	nextHeight      int64
	committedHeight int64
	appHash         common.Hash
	parentHash      common.Hash
	// lastBlockTime is the Time of the most recently committed block.
	lastBlockTime uint64
	pending       utils.Option[evmOnlyPending]
}

type evmOnlyPending struct {
	height    int64
	appHash   common.Hash
	blockHash common.Hash
	timestamp uint64
}

var _ abci.Application = (*evmOnlyApplication)(nil)

// NewEVMOnlyApplication returns the raw-Ethereum application used by Autobahn
// load tests. State, receipts, and blocks are owned by storage.
func NewEVMOnlyApplication(
	chainID uint64,
	validators []abci.ValidatorUpdate,
	storage *bootstrap.GigaStorageManager,
	changeSetEncoder evmonly.NamedChangeSetEncoder,
) abci.Application {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	return &evmOnlyApplication{
		chainID:          new(big.Int).SetUint64(chainID),
		chainConfig:      &chainConfig,
		storage:          storage,
		changeSetEncoder: changeSetEncoder,
		validators:       slices.Clone(validators),
		state:            utils.NewMutex(&evmOnlyState{}),
	}
}

func (a *evmOnlyApplication) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("EVM-only initial height must be positive: %d", req.InitialHeight)
	}
	gasLimit, err := evmOnlyGasLimit(req)
	if err != nil {
		return nil, err
	}
	if err := a.seedInitialStateVersion(req.InitialHeight); err != nil {
		return nil, err
	}
	for state := range a.state.Lock() {
		if state.executor.IsPresent() {
			return nil, fmt.Errorf("EVM-only application already initialized")
		}
		state.executor = utils.Some(evmonly.NewExecutor(evmonly.Config{
			ChainConfig:         a.chainConfig,
			MinGasPrice:         big.NewInt(evmOnlyMinGasPrice),
			OCCWorkers:          runtime.GOMAXPROCS(0),
			ParseWorkers:        runtime.GOMAXPROCS(0),
			BlockResultPoolSize: 1,
		},
			evmonly.WithStorageManager(a.storage, a.changeSetEncoder),
			evmonly.WithMissingAccountState(evmOnlyFundedState{}),
		))
		state.gasLimit = gasLimit
		state.nextHeight = req.InitialHeight
		state.committedHeight = req.InitialHeight - 1
		return &abci.ResponseInitChain{}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) seedInitialStateVersion(initialHeight int64) error {
	stateStore := a.storage.SC()
	if stateStore == nil || initialHeight == 1 {
		return nil
	}
	latest, err := stateStore.GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read EVM-only state version: %w", err)
	}
	if latest != 0 {
		return fmt.Errorf("EVM-only state is already at height %d before InitChain", latest)
	}
	if err := stateStore.SetInitialVersion(initialHeight); err != nil {
		return fmt.Errorf("seed EVM-only initial state version %d: %w", initialHeight, err)
	}
	return nil
}

func evmOnlyGasLimit(req *abci.RequestInitChain) (uint64, error) {
	if req.ConsensusParams == nil || req.ConsensusParams.Block == nil || req.ConsensusParams.Block.MaxGas <= 0 {
		return 0, fmt.Errorf("EVM-only max gas must be positive")
	}
	gasLimit, ok := utils.SafeCast[uint64](req.ConsensusParams.Block.MaxGas)
	if !ok {
		return 0, fmt.Errorf("EVM-only max gas exceeds uint64: %d", req.ConsensusParams.Block.MaxGas)
	}
	return gasLimit, nil
}

func (a *evmOnlyApplication) Info() *abci.ResponseInfo {
	for state := range a.state.Lock() {
		return &abci.ResponseInfo{
			Data:             "evmonly",
			LastBlockHeight:  state.committedHeight,
			LastBlockAppHash: append([]byte(nil), state.appHash[:]...),
		}
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) LastBlockHeight() int64 {
	for state := range a.state.Lock() {
		return state.committedHeight
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) GetValidators() []abci.ValidatorUpdate {
	return slices.Clone(a.validators)
}

func (a *evmOnlyApplication) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
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
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    gasWanted,
			GasEstimated: gasWanted,
		},
		IsEVM:            true,
		EVMNonce:         tx.Nonce(),
		EVMHash:          tx.Hash(),
		EVMSenderAddress: sender,
		SeiSenderAddress: append([]byte(nil), sender[:]...),
	}
}

func (a *evmOnlyApplication) parseTx(raw []byte) (*ethtypes.Transaction, common.Address, error) {
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
	// The predicate block validity uses, not tx.GasPrice(): on a dynamic-fee tx
	// that is the fee cap, so admitting on it let through transactions the
	// executor then refused — and an executor refusal is a node panic, not a
	// failed receipt.
	if evmonly.EffectiveGasPrice(tx, evmOnlyBaseFee()).Cmp(big.NewInt(evmOnlyMinGasPrice)) < 0 {
		return nil, common.Address{}, fmt.Errorf("ethereum transaction effective gas price is below %d", evmOnlyMinGasPrice)
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(a.chainID), tx)
	if err != nil {
		return nil, common.Address{}, err
	}
	return tx, sender, nil
}

func evmOnlyStoreAddress(address common.Address) gigatypes.Address {
	var storeAddress gigatypes.Address
	copy(storeAddress[:], address[:])
	return storeAddress
}

func (a *evmOnlyApplication) EvmNonce(address common.Address) uint64 {
	snapshot := a.storage.StateDB().OpenView()
	defer snapshot.Close()
	return snapshot.GetNonce(evmOnlyStoreAddress(address))
}

func (a *evmOnlyApplication) EvmBalance(address common.Address, _ []byte) uint256.Int {
	snapshot := a.storage.StateDB().OpenView()
	defer snapshot.Close()
	if !snapshot.AccountExists(evmOnlyStoreAddress(address)) {
		return *uint256.MustFromBig(evmOnlyBaseBalance)
	}
	balance := snapshot.GetBalance(evmOnlyStoreAddress(address))
	return *new(uint256.Int).SetBytes(balance[:])
}

func (a *evmOnlyApplication) EvmChainID() uint64 {
	return a.chainID.Uint64()
}

// evmOnlyPrevRandao derives a deterministic PrevRandao from a block timestamp.
func evmOnlyPrevRandao(timestamp uint64) common.Hash {
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, timestamp))
}

// EvmCall executes msg as a read-only call against the most recently
// committed EVM state and returns the execution result.
func (a *evmOnlyApplication) EvmCall(ctx context.Context, msg *ethcore.Message) (*ethcore.ExecutionResult, error) {
	var executor *evmonly.Executor
	var blockCtx evmonly.BlockContext
	for state := range a.state.Lock() {
		exec, ok := state.executor.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only call attempted before InitChain")
		}
		if state.pending.IsPresent() {
			// FinalizeBlock already wrote this block's state to the store, but
			// committedHeight/lastBlockTime (below) only advance on Commit, so a
			// call in this window would see the new block's storage under the
			// previous block's NUMBER/TIMESTAMP/PrevRandao.
			return nil, fmt.Errorf("EVM-only call attempted before committing the finalized block")
		}
		number, ok := utils.SafeCast[uint64](state.committedHeight)
		if !ok {
			return nil, fmt.Errorf("EVM-only committed height exceeds uint64: %d", state.committedHeight)
		}
		executor = exec
		// Coinbase and ParentHash are left zero: no coinbase is tracked outside
		// FinalizeBlock, and only the current block's hash is tracked at all.
		blockCtx = evmonly.BlockContext{
			Number:      number,
			Time:        state.lastBlockTime,
			GasLimit:    state.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			BlockHash:   state.parentHash,
			PrevRandao:  evmOnlyPrevRandao(state.lastBlockTime),
		}
	}
	return executor.Call(ctx, blockCtx, msg)
}

func (a *evmOnlyApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
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
	for state := range a.state.Lock() {
		executor, ok := state.executor.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		if state.pending.IsPresent() {
			return nil, fmt.Errorf("EVM-only block %d finalized before committing the previous block", height)
		}
		if height != state.nextHeight {
			return nil, fmt.Errorf("EVM-only block height %d does not match next height %d", height, state.nextHeight)
		}
		result, err := executor.ExecuteBlock(ctx, evmonly.BlockRequest{
			Context: evmonly.BlockContext{
				Number:      number,
				Time:        timestamp,
				GasLimit:    state.gasLimit,
				ChainID:     new(big.Int).Set(a.chainID),
				BaseFee:     evmOnlyBaseFee(),
				BlobBaseFee: new(big.Int),
				ParentHash:  state.parentHash,
				BlockHash:   blockHash,
				PrevRandao:  evmOnlyPrevRandao(timestamp),
			},
			Txs: req.Txs,
		})
		if err != nil {
			return nil, err
		}
		defer result.Release()
		appHash, err := hashEVMOnlyResult(state.appHash, number, blockHash, result)
		if err != nil {
			return nil, err
		}
		state.pending = utils.Some(evmOnlyPending{height: height, appHash: appHash, blockHash: blockHash, timestamp: timestamp})
		return &abci.ResponseFinalizeBlock{
			AppHash:   append([]byte(nil), appHash[:]...),
			TxResults: evmOnlyABCIResults(result),
		}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.state.Lock() {
		pending, ok := state.pending.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only Commit called without a finalized block")
		}
		state.committedHeight = pending.height
		state.nextHeight = pending.height + 1
		state.appHash = pending.appHash
		state.parentHash = pending.blockHash
		state.lastBlockTime = pending.timestamp
		state.pending = utils.None[evmOnlyPending]()
		return &abci.ResponseCommit{}, nil
	}
	panic("unreachable")
}

func evmOnlyABCIResults(result *evmonly.BlockResult) []*abci.ExecTxResult {
	txResults := make([]*abci.ExecTxResult, len(result.Txs))
	for i, tx := range result.Txs {
		gasUsed := utils.Clamp[int64](tx.GasUsed)
		txResults[i] = &abci.ExecTxResult{
			Code:      abci.CodeTypeOK,
			GasWanted: gasUsed,
			GasUsed:   gasUsed,
		}
	}
	return txResults
}

func hashEVMOnlyResult(previous common.Hash, height uint64, blockHash common.Hash, result *evmonly.BlockResult) (common.Hash, error) {
	h := sha256.New()
	_, _ = h.Write(previous[:])
	_, _ = h.Write(binary.BigEndian.AppendUint64(nil, height))
	_, _ = h.Write(blockHash[:])
	_, _ = h.Write(binary.BigEndian.AppendUint64(nil, result.GasUsed))
	changesets, err := evmonly.EncodeMemoryStoreChangeSet(result.ChangeSet)
	if err != nil {
		return common.Hash{}, err
	}
	for _, changeset := range changesets {
		writeEVMOnlyHashBytes(h, []byte(changeset.Name))
		for _, pair := range changeset.Changeset.Pairs {
			writeEVMOnlyHashBytes(h, pair.Key)
			if pair.Delete {
				_, _ = h.Write([]byte{1})
			} else {
				_, _ = h.Write([]byte{0})
			}
			writeEVMOnlyHashBytes(h, pair.Value)
		}
	}
	return common.BytesToHash(h.Sum(nil)), nil
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeEVMOnlyHashBytes(w byteWriter, value []byte) {
	_, _ = w.Write(binary.BigEndian.AppendUint64(nil, uint64(len(value))))
	_, _ = w.Write(value)
}

type evmOnlyFundedState struct{}

func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
