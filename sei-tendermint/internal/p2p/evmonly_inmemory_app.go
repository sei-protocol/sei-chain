package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"runtime"

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
	state       utils.Mutex[*evmOnlyInMemoryState]
}

type evmOnlyInMemoryState struct {
	executor        utils.Option[*evmonly.Executor]
	gasLimit        uint64
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

var _ abci.Application = (*evmOnlyInMemoryApplication)(nil)

// NewEVMOnlyInMemoryApplication returns an ephemeral raw-Ethereum application for
// Autobahn Docker load tests.
func NewEVMOnlyInMemoryApplication(chainID uint64) abci.Application {
	base := evmOnlyFundedState{}
	store := evmonly.NewMemoryStore(base)
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(chainID)
	return &evmOnlyInMemoryApplication{
		chainID:     new(big.Int).SetUint64(chainID),
		chainConfig: &chainConfig,
		store:       store,
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
		if state.executor.IsPresent() {
			return nil, fmt.Errorf("EVM-only application already initialized")
		}
		state.executor = utils.Some(evmonly.NewExecutor(evmonly.Config{
			ChainConfig:         a.chainConfig,
			MinGasPrice:         big.NewInt(evmOnlyInMemoryMinGasPrice),
			OCCWorkers:          runtime.GOMAXPROCS(0),
			ParseWorkers:        runtime.GOMAXPROCS(0),
			BlockResultPoolSize: 1,
		}, evmonly.WithStore(a.store, a.store.EncodeChangeSet)))
		state.gasLimit = gasLimit
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

func (a *evmOnlyInMemoryApplication) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
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

func (a *evmOnlyInMemoryApplication) parseTx(raw []byte) (*ethtypes.Transaction, common.Address, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, common.Address{}, err
	}
	if !tx.Protected() {
		return nil, common.Address{}, fmt.Errorf("unprotected Ethereum transaction")
	}
	if tx.ChainId().Cmp(a.chainID) != 0 {
		return nil, common.Address{}, fmt.Errorf("Ethereum transaction chain ID does not match %s", a.chainID)
	}
	if tx.Type() == ethtypes.BlobTxType {
		return nil, common.Address{}, fmt.Errorf("blob transactions are not supported")
	}
	if tx.GasPrice().Cmp(big.NewInt(evmOnlyInMemoryMinGasPrice)) < 0 {
		return nil, common.Address{}, fmt.Errorf("Ethereum transaction gas price is below %d", evmOnlyInMemoryMinGasPrice)
	}
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(a.chainID), tx)
	if err != nil {
		return nil, common.Address{}, err
	}
	return tx, sender, nil
}

func (a *evmOnlyInMemoryApplication) EvmNonce(address common.Address) uint64 {
	snapshot := a.store.OpenView()
	defer snapshot.Close()
	return snapshot.GetNonce(gigastore.Address(address))
}

func (a *evmOnlyInMemoryApplication) EvmBalance(address common.Address, _ []byte) uint256.Int {
	snapshot := a.store.OpenView()
	defer snapshot.Close()
	balance := snapshot.GetBalance(gigastore.Address(address))
	return *new(uint256.Int).SetBytes(balance[:])
}

func (a *evmOnlyInMemoryApplication) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
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
				BaseFee:     new(big.Int),
				BlobBaseFee: new(big.Int),
				ParentHash:  state.parentHash,
				BlockHash:   blockHash,
				PrevRandao:  crypto.Keccak256Hash(binary.BigEndian.AppendUint64(nil, timestamp)),
			},
			Txs: req.Txs,
		})
		if err != nil {
			return nil, err
		}
		defer result.Release()
		appHash, err := hashEVMOnlyInMemoryResult(state.appHash, number, blockHash, result)
		if err != nil {
			return nil, err
		}
		state.pending = utils.Some(evmOnlyInMemoryPending{height: height, appHash: appHash, blockHash: blockHash})
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
	for i, tx := range result.Txs {
		gasUsed := min(tx.GasUsed, math.MaxInt64)
		txResults[i] = &abci.ExecTxResult{
			Code:      abci.CodeTypeOK,
			GasWanted: int64(gasUsed),
			GasUsed:   int64(gasUsed),
		}
	}
	return txResults
}

func hashEVMOnlyInMemoryResult(previous common.Hash, height uint64, blockHash common.Hash, result *evmonly.BlockResult) (common.Hash, error) {
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

func (evmOnlyFundedState) AccountExists(common.Address) bool { return true }
func (evmOnlyFundedState) GetBalance(common.Address) *big.Int {
	return new(big.Int).Set(evmOnlyInMemoryBaseBalance)
}
func (evmOnlyFundedState) GetNonce(common.Address) uint64                   { return 0 }
func (evmOnlyFundedState) GetCode(common.Address) []byte                    { return nil }
func (evmOnlyFundedState) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
