package evm

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// Application executes EVM transactions for Autobahn and serves read-only
// EVM calls and execution parameters.
type Application interface {
	// Application state and initialization.
	Info() *ResponseInfo
	EpochRegistry() (*atypes.EpochRegistry, error)
	LastBlockHeight() int64
	InitChain(InitConfig) error
	RestoreBlockTime(time.Time)

	// Transaction admission and block execution.
	// CheckTx returns admission metadata or an error rejecting the transaction.
	CheckTx(EvmTx) (AdmittedTx, error)
	EvmBalance(common.Address) uint256.Int
	EvmNonce(common.Address) uint64
	// EvmCommittedNonce returns addr's nonce in the last committed EVM state without waiting for a
	// commit in flight, so it can trail EvmNonce by the most recently finalized block.
	EvmCommittedNonce(common.Address) uint64
	// FinalizeBlock executes the block at req.Height. It may be called before the call for the
	// previous block returns; it then waits for it, and ctx cancels only that wait.
	FinalizeBlock(context.Context, FinalizeBlockReq) (FinalizeBlockResp, error)
	// AwaitCommits waits for finalized blocks to reach the store and returns any commit failure.
	AwaitCommits() error

	// EvmCode returns the contract code at address in the current EVM state.
	EvmCode(address common.Address) []byte
	// EvmStorage returns the value at key in address's current EVM storage.
	EvmStorage(address common.Address, key common.Hash) common.Hash
	// EvmBalanceAt returns address's balance in the committed EVM state after height.
	EvmBalanceAt(address common.Address, height int64) (uint256.Int, error)
	// EvmCodeAt returns the contract code at address in the committed EVM state after height.
	EvmCodeAt(address common.Address, height int64) ([]byte, error)
	// EvmNonceAt returns address's nonce in the committed EVM state after height.
	EvmNonceAt(address common.Address, height int64) (uint64, error)
	// EvmStorageAt returns the value at key in address's committed EVM storage after height.
	EvmStorageAt(address common.Address, key common.Hash, height int64) (common.Hash, error)

	// EvmCall executes msg as a read-only call against the current EVM state,
	// with overrides applied.
	EvmCall(ctx context.Context, msg *core.Message, overrides CallOverrides) (*core.ExecutionResult, error)
	// EvmEstimateGas returns the lowest gas limit at or below gasCap that lets
	// msg succeed with overrides applied, and any revert data.
	EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64, overrides CallOverrides) (uint64, []byte, error)
	// EvmCallAt executes msg as a read-only call against the committed EVM
	// state after block, in the context that block executed at, with
	// overrides applied.
	EvmCallAt(ctx context.Context, msg *core.Message, block BlockRef, overrides CallOverrides) (*core.ExecutionResult, error)
	// EvmEstimateGasAt returns the lowest gas limit at or below gasCap that
	// lets msg succeed against the committed EVM state after block,
	// with overrides applied, and any revert data.
	EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, block BlockRef, overrides CallOverrides) (uint64, []byte, error)
	// EvmEstimateGasAfterCalls returns what EvmEstimateGas returns for msg with overrides and
	// then calls applied to the current EVM state first, in order.
	EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, overrides CallOverrides) (uint64, []byte, error)
	// EvmEstimateGasAfterCallsAt returns what EvmEstimateGasAt returns for msg with overrides and
	// then calls applied to the committed EVM state after block first, in order.
	EvmEstimateGasAfterCallsAt(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, block BlockRef, overrides CallOverrides) (uint64, []byte, error)
	// EvmCreateAccessList returns the access list msg touches against the current EVM state, and
	// the result of executing msg with that list.
	EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error)
	// EvmCreateAccessListAt returns the access list msg touches against the committed EVM state
	// after block, in the context that block executed at, and the result of executing
	// msg with that list.
	EvmCreateAccessListAt(ctx context.Context, msg *core.Message, block BlockRef) (ethtypes.AccessList, *core.ExecutionResult, error)
	// EvmChainID returns the EVM chain ID transactions are signed for.
	EvmChainID() uint64
	// EvmChainConfig returns the EVM chain configuration transactions execute
	// against.
	EvmChainConfig() *params.ChainConfig
	// EvmBaseFee returns the base fee every block executes at.
	EvmBaseFee() *big.Int
	// EvmGasLimit returns the gas limit of the most recently committed block.
	EvmGasLimit() uint64
	// EvmMinGasPrice returns the minimum effective gas price a transaction is
	// admitted at.
	EvmMinGasPrice() *big.Int
	// EvmBlockContext returns the block context block executed at.
	EvmBlockContext(block BlockRef) (vm.BlockContext, error)
	// EvmStateAt returns the committed EVM state after height, or the latest
	// committed state when height is rpc.LatestBlockNumber, held until the returned handle is closed.
	EvmStateAt(ctx context.Context, height int64) (*StateHandle, error)
	// EvmStateBeforeTx returns the EVM state the transaction at index len(txs)
	// of block executed against, held until the returned handle is closed.
	EvmStateBeforeTx(ctx context.Context, block BlockRef, txs [][]byte) (*StateHandle, error)
}
