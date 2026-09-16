package evmonlyapp

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
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
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const evmOnlyMinGasPrice = 1_000_000_000

// evmOnlyBaseFee is the base fee this application executes every block at.
// Admission and block validity both price against it, so they cannot diverge.
func evmOnlyBaseFee() *big.Int { return new(big.Int) }

var evmOnlyBaseBalance = new(big.Int).Lsh(big.NewInt(1), 200)

// checkedSendersCap bounds the senders remembered from CheckTx. Entries are
// dropped as their transactions execute; the cap only guards against admitted
// transactions that never reach a block.
const checkedSendersCap = 1 << 18

type evmOnlyApplication struct {
	abci.BaseApplication

	chainID          *big.Int
	chainConfig      *params.ChainConfig
	storage          *bootstrap.GigaStorageManager
	changeSetEncoder evmonly.NamedChangeSetEncoder
	validators       []abci.ValidatorUpdate
	executor         utils.Mutex[*utils.Option[*evmonly.Executor]]
	// Lock order: executor before cursor. FinalizeBlock holds executor while
	// the block's cursor encoder takes cursor.
	cursor utils.Mutex[*evmOnlyCursorState]
	// checkedSenders maps the hash of every transaction this process admitted
	// in CheckTx to the sender recovered there, so execution does not recover
	// it again.
	checkedSenders utils.Mutex[map[common.Hash]common.Address]
}

// evmOnlyCursorState is the execution position: the block whose state is
// committed to storage and the block finalized but not yet acknowledged by
// Commit.
type evmOnlyCursorState struct {
	committed evmOnlyCursor
	pending   utils.Option[evmOnlyCursor]
	// lastBlockTime is the Time of the most recently committed block, used by
	// EvmCall to reproduce that block's execution context for a read-only call.
	lastBlockTime uint64
	// pendingBlockTime is the Time of the block staged in pending; Commit
	// promotes it to lastBlockTime.
	pendingBlockTime uint64
}

var _ abci.Application = (*evmOnlyApplication)(nil)

// NewEVMOnlyApplication returns the raw-Ethereum application used by Autobahn
// load tests. State, receipts, and blocks are owned by storage. A storage that
// already holds committed blocks resumes from its durable cursor, so Info
// reports the stored height and InitChain is refused.
func NewEVMOnlyApplication(
	chainID uint64,
	validators []abci.ValidatorUpdate,
	storage *bootstrap.GigaStorageManager,
	changeSetEncoder evmonly.NamedChangeSetEncoder,
) (abci.Application, error) {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	a := &evmOnlyApplication{
		chainID:          new(big.Int).SetUint64(chainID),
		chainConfig:      &chainConfig,
		storage:          storage,
		changeSetEncoder: changeSetEncoder,
		validators:       slices.Clone(validators),
		executor:         utils.NewMutex(new(utils.Option[*evmonly.Executor])),
		cursor:           utils.NewMutex(&evmOnlyCursorState{}),
		checkedSenders:   utils.NewMutex(map[common.Hash]common.Address{}),
	}
	cursor, err := loadEVMOnlyCursor(storage.SC())
	if err != nil {
		return nil, err
	}
	if cursor, ok := cursor.Get(); ok {
		for executor := range a.executor.Lock() {
			*executor = utils.Some(a.newExecutor())
		}
		for state := range a.cursor.Lock() {
			state.committed = cursor
		}
	}
	return a, nil
}

func (a *evmOnlyApplication) newExecutor() *evmonly.Executor {
	return evmonly.NewExecutor(evmonly.Config{
		ChainConfig:         a.chainConfig,
		MinGasPrice:         big.NewInt(evmOnlyMinGasPrice),
		OCCWorkers:          runtime.GOMAXPROCS(0),
		ParseWorkers:        runtime.GOMAXPROCS(0),
		BlockResultPoolSize: 1,
	},
		evmonly.WithStorageManager(a.storage, a.changeSetEncoder),
		evmonly.WithMissingAccountState(evmOnlyFundedState{}),
		evmonly.WithBlockChangeSetEncoder(a.encodeCursorChangeSet),
	)
}

// encodeCursorChangeSet chains the block into the app hash and stages the
// resulting cursor as pending, returning it as the changeset committed with
// the block's state.
func (a *evmOnlyApplication) encodeCursorChangeSet(block evmonly.BlockContext, result *evmonly.BlockResult) ([]*proto.NamedChangeSet, error) {
	height, ok := utils.SafeCast[int64](block.Number)
	if !ok {
		return nil, fmt.Errorf("EVM-only block number exceeds int64: %d", block.Number)
	}
	for state := range a.cursor.Lock() {
		if height != state.committed.height+1 {
			return nil, fmt.Errorf("EVM-only block height %d does not follow committed height %d", height, state.committed.height)
		}
		appHash, err := hashEVMOnlyResult(state.committed.appHash, block.Number, block.BlockHash, result)
		if err != nil {
			return nil, err
		}
		next := evmOnlyCursor{
			height:    height,
			appHash:   appHash,
			blockHash: block.BlockHash,
			gasLimit:  block.GasLimit,
		}
		state.pending = utils.Some(next)
		state.pendingBlockTime = block.Time
		return []*proto.NamedChangeSet{next.changeSet()}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("EVM-only initial height must be positive: %d", req.InitialHeight)
	}
	gasLimit, err := evmOnlyGasLimit(req)
	if err != nil {
		return nil, err
	}
	for executor := range a.executor.Lock() {
		if executor.IsPresent() {
			return nil, fmt.Errorf("EVM-only application already initialized")
		}
		if err := a.seedInitialStateVersion(req.InitialHeight); err != nil {
			return nil, err
		}
		*executor = utils.Some(a.newExecutor())
		for state := range a.cursor.Lock() {
			state.committed = evmOnlyCursor{height: req.InitialHeight - 1, gasLimit: gasLimit}
		}
		return &abci.ResponseInitChain{}, nil
	}
	panic("unreachable")
}

// seedInitialStateVersion moves an empty state store to the version preceding
// initialHeight. A store already seeded there is accepted, since a crash
// between InitChain and the first block leaves it that way; a store holding
// any block is refused.
func (a *evmOnlyApplication) seedInitialStateVersion(initialHeight int64) error {
	stateStore := a.storage.SC()
	latest, err := stateStore.GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read EVM-only state version: %w", err)
	}
	switch {
	case latest == initialHeight-1:
		return nil
	case latest != 0:
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
	for state := range a.cursor.Lock() {
		return &abci.ResponseInfo{
			Data:             "evmonly",
			LastBlockHeight:  state.committed.height,
			LastBlockAppHash: append([]byte(nil), state.committed.appHash[:]...),
		}
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) LastBlockHeight() int64 {
	for state := range a.cursor.Lock() {
		return state.committed.height
	}
	panic("unreachable")
}

// EvmGasLimit returns the gas limit of the most recently committed block.
// This application never changes it after InitChain, so it is also the gas
// limit of every earlier committed block.
func (a *evmOnlyApplication) EvmGasLimit() uint64 {
	for state := range a.cursor.Lock() {
		return state.committed.gasLimit
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
	a.rememberSender(tx.Hash(), sender)
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

func (a *evmOnlyApplication) rememberSender(hash common.Hash, sender common.Address) {
	for senders := range a.checkedSenders.Lock() {
		if len(senders) >= checkedSendersCap {
			clear(senders)
		}
		senders[hash] = sender
	}
}

// takeSenders returns, aligned with txs, the sender CheckTx recovered for each
// transaction this process admitted, and forgets those entries. The hash of a
// raw transaction is the keccak of its bytes for every transaction type, so no
// decoding is needed.
func (a *evmOnlyApplication) takeSenders(txs [][]byte) []utils.Option[common.Address] {
	out := make([]utils.Option[common.Address], len(txs))
	for senders := range a.checkedSenders.Lock() {
		for i, raw := range txs {
			hash := crypto.Keccak256Hash(raw)
			if sender, ok := senders[hash]; ok {
				out[i] = utils.Some(sender)
				delete(senders, hash)
			}
		}
	}
	return out
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

// EvmChainConfig returns the EVM chain configuration this node executes against.
func (a *evmOnlyApplication) EvmChainConfig() *params.ChainConfig {
	return a.chainConfig
}

// EvmBaseFee returns the base fee this application executes every block at.
func (a *evmOnlyApplication) EvmBaseFee() *big.Int {
	return evmOnlyBaseFee()
}

// EvmMinGasPrice returns the minimum effective gas price this application
// admits a transaction at.
func (a *evmOnlyApplication) EvmMinGasPrice() *big.Int {
	return big.NewInt(evmOnlyMinGasPrice)
}

// evmOnlyPrevRandao derives a deterministic PrevRandao from a block timestamp.
func evmOnlyPrevRandao(timestamp uint64) common.Hash {
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, timestamp))
}

// EvmCall executes msg as a read-only call against the most recently
// committed EVM state and returns the execution result.
func (a *evmOnlyApplication) EvmCall(ctx context.Context, msg *ethcore.Message) (*ethcore.ExecutionResult, error) {
	var executor *evmonly.Executor
	for exec := range a.executor.Lock() {
		got, ok := exec.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only call attempted before InitChain")
		}
		executor = got
	}
	var blockCtx evmonly.BlockContext
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			// The store already has this block's writes; NUMBER/TIMESTAMP/PrevRandao advance only on Commit.
			return nil, fmt.Errorf("EVM-only call attempted before committing the finalized block")
		}
		number, ok := utils.SafeCast[uint64](state.committed.height)
		if !ok {
			return nil, fmt.Errorf("EVM-only committed height exceeds uint64: %d", state.committed.height)
		}
		// Coinbase and ParentHash are left zero: no coinbase is tracked outside
		// FinalizeBlock, and only the current block's hash is tracked at all.
		blockCtx = evmonly.BlockContext{
			Number:      number,
			Time:        state.lastBlockTime,
			GasLimit:    state.committed.gasLimit,
			ChainID:     new(big.Int).Set(a.chainID),
			BaseFee:     evmOnlyBaseFee(),
			BlobBaseFee: new(big.Int),
			BlockHash:   state.committed.blockHash,
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
	for executor := range a.executor.Lock() {
		executor, ok := executor.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only block finalized before InitChain")
		}
		parent, err := a.beginBlock(height)
		if err != nil {
			return nil, err
		}
		result, err := executor.ExecuteBlock(ctx, evmonly.BlockRequest{
			Context: evmonly.BlockContext{
				Number:      number,
				Time:        timestamp,
				GasLimit:    parent.gasLimit,
				ChainID:     new(big.Int).Set(a.chainID),
				BaseFee:     evmOnlyBaseFee(),
				BlobBaseFee: new(big.Int),
				ParentHash:  parent.blockHash,
				BlockHash:   blockHash,
				PrevRandao:  evmOnlyPrevRandao(timestamp),
			},
			Txs:     req.Txs,
			Senders: a.takeSenders(req.Txs),
		})
		if err != nil {
			return nil, errors.Join(err, a.abandonPending(height))
		}
		defer result.Release()
		pending, err := a.pendingCursor(height)
		if err != nil {
			return nil, err
		}
		return &abci.ResponseFinalizeBlock{
			AppHash:   append([]byte(nil), pending.appHash[:]...),
			TxResults: evmOnlyABCIResults(result),
		}, nil
	}
	panic("unreachable")
}

// beginBlock checks height is the next block to finalize and returns the
// committed cursor it builds on.
func (a *evmOnlyApplication) beginBlock(height int64) (evmOnlyCursor, error) {
	for state := range a.cursor.Lock() {
		if state.pending.IsPresent() {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block %d finalized before committing the previous block", height)
		}
		if next := state.committed.height + 1; height != next {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block height %d does not match next height %d", height, next)
		}
		return state.committed, nil
	}
	panic("unreachable")
}

// abandonPending drops the cursor staged by a failed block unless the store
// already holds that block's version, in which case the cursor is durable and
// stays pending for Commit.
func (a *evmOnlyApplication) abandonPending(height int64) error {
	latest, err := a.storage.SC().GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read EVM-only state version: %w", err)
	}
	if latest >= height {
		return nil
	}
	for state := range a.cursor.Lock() {
		state.pending = utils.None[evmOnlyCursor]()
	}
	return nil
}

func (a *evmOnlyApplication) pendingCursor(height int64) (evmOnlyCursor, error) {
	for state := range a.cursor.Lock() {
		pending, ok := state.pending.Get()
		if !ok || pending.height != height {
			return evmOnlyCursor{}, fmt.Errorf("EVM-only block %d committed without staging its cursor", height)
		}
		return pending, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.cursor.Lock() {
		pending, ok := state.pending.Get()
		if !ok {
			return nil, fmt.Errorf("EVM-only Commit called without a finalized block")
		}
		state.committed = pending
		state.lastBlockTime = state.pendingBlockTime
		state.pending = utils.None[evmOnlyCursor]()
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
