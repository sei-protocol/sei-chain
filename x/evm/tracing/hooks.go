package tracing

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// BlockEvent is emitted upon tracing an incoming block.
// It contains the block as well as consensus related information.
type BlockEvent struct {
	Hash   common.Hash
	Header *types.Header
	Size   uint64
}

type (
	// OnSeiBlockchainInitHook is called when the blockchain is initialized
	// once per process and receives the chain configuration.
	OnSeiBlockchainInitHook = func(chainConfig *params.ChainConfig)
	// OnSeiBlockStart is called before executing `block`.
	// `td` is the total difficulty prior to `block`.
	// `skip` indicates processing of this previously known block
	// will be skipped. OnBlockStart and OnBlockEnd will be emitted to
	// convey how chain is progressing. E.g. known blocks will be skipped
	// when node is started after a crash.
	OnSeiBlockStartHook = func(hash []byte, size uint64, b *types.Header)
	// OnSeiBlockEndHook is called after executing `block` and receives the error
	// that occurred during processing. If an `err` is received in the callback,
	// it means the block should be discarded (optimistic execution failed for example).
	OnSeiBlockEndHook = func(err error)

	// OnSeiSystemCallStart is called before executing a system call in Sei context. CoWasm
	// contract execution that invokes the EVM from one ore another are examples of system calls.
	//
	// You can use this hook to route upcoming `OnEnter/OnExit` EVM tracing hook to be appended
	// to a system call bucket for the purpose of tracing the system calls.
	OnSeiSystemCallStartHook = func()

	// OnSeiSystemCallStart is called after executing a system call in Sei context. CoWasm
	// contract execution that invokes the EVM from one ore another are examples of system calls.
	//
	// You can use this hook to terminate special handling of `OnEnter/OnExit`.
	OnSeiSystemCallEndHook = func()

	OnSeiPostTxCosmosEventsHook = func(addedLogs []*evmtypes.Log, newReceipt *evmtypes.Receipt, onEvmTransaction bool)
)

// Hooks is used to collect traces during chain processing. It's a similar
// interface as the go-ethereum's [tracing.Hooks] but adapted to Sei particularities.
//
// The method all starts with OnSei... to avoid confusion with the go-ethereum's [tracing.Hooks]
// interface and allow one to implement both interfaces in the same struct.
type Hooks struct {
	*tracing.Hooks

	OnSeiBlockchainInit OnSeiBlockchainInitHook
	OnSeiBlockStart     OnSeiBlockStartHook
	OnSeiBlockEnd       OnSeiBlockEndHook

	OnSeiSystemCallStart OnSeiSystemCallStartHook
	OnSeiSystemCallEnd   func()

	OnSeiPostTxCosmosEvents OnSeiPostTxCosmosEventsHook

	GetTxTracer func(txIndex int) sdk.TxTracer
}

var _ sdk.TxTracer = (*Hooks)(nil)

func (h *Hooks) InjectInContext(ctx sdk.Context) sdk.Context {
	return ctx
}

func (h *Hooks) Reset() {
}

func (h *Hooks) Commit() {
}
