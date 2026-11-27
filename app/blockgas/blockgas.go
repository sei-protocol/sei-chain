// Package blockgas holds the transactions of a block to the block's gas limits.
package blockgas

import (
	"math"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmante "github.com/sei-protocol/sei-chain/x/evm/ante"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// Kind is how a transaction is charged against its block's gas limits.
type Kind int

const (
	// Uncharged is a transaction that is never processed or consumes no gas.
	Uncharged Kind = iota
	// Cosmos is a Cosmos transaction, charged its whole gas limit before its block executes.
	Cosmos
	// EVM is an EVM transaction, charged the gas it uses as its block executes.
	EVM
)

// Of returns how tx is charged against its block's gas limits and the gas limit it declares. It
// returns an error for an EVM transaction whose payload cannot be unpacked.
func Of(tx sdk.Tx) (Kind, uint64, error) {
	if tx == nil {
		return Uncharged, 0, nil
	}
	isEVM, err := evmante.IsEVMMessage(tx)
	if err != nil {
		// A transaction mixing an EVM message with other messages is never processed.
		return Uncharged, 0, nil
	}
	if !isEVM {
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			// A transaction without a fee is never processed.
			return Uncharged, 0, nil
		}
		return Cosmos, feeTx.GetGas(), nil
	}
	txData, err := evmtypes.UnpackTxData(evmtypes.MustGetEVMTransactionMessage(tx).Data)
	if err != nil {
		return Uncharged, 0, err
	}
	if _, ok := txData.(*ethtx.AssociateTx); ok {
		return Uncharged, 0, nil
	}
	return EVM, txData.GetGas(), nil
}

// Limit returns the MaxGas or MaxGasWanted consensus parameter param as an amount of gas, with a
// negative value meaning no limit.
func Limit(param int64) uint64 {
	if param < 0 {
		return math.MaxUint64
	}
	return uint64(param) //nolint:gosec // param is non-negative here
}

// Pool is the EVM gas a block has left as its transactions execute in order. It starts at what
// the gas limits of the block's Cosmos transactions leave of MaxGas.
type Pool struct {
	kinds     []Kind
	limits    []uint64
	left      uint64
	mayReject bool
}

// NewPool returns the pool of a block of txs under the consensus params cp, where nil params
// mean no limit.
func NewPool(cp *tmproto.ConsensusParams, txs []sdk.Tx) *Pool {
	left := uint64(math.MaxUint64)
	if cp != nil && cp.Block != nil {
		left = Limit(cp.Block.MaxGas)
	}
	p := &Pool{kinds: make([]Kind, len(txs)), limits: make([]uint64, len(txs))}
	var evmLimits uint64
	for i, tx := range txs {
		kind, limit, err := Of(tx)
		if err != nil {
			// The transaction fails when it is delivered, without executing.
			continue
		}
		p.kinds[i], p.limits[i] = kind, limit
		switch kind {
		case Cosmos:
			left -= min(limit, left)
		case EVM:
			evmLimits += min(limit, math.MaxUint64-evmLimits)
		}
	}
	p.left = left
	p.mayReject = evmLimits > left
	return p
}

// MayReject reports whether the gas limits of the block's EVM transactions sum to more than the
// pool starts with. Admit refuses no transaction when they do not.
func (p *Pool) MayReject() bool {
	return p.mayReject
}

// Admit returns an error wrapping ErrBlockGasLimitReached if the transaction at index i is an EVM
// transaction whose gas limit exceeds the gas left, and nil otherwise.
func (p *Pool) Admit(i int) error {
	if i < 0 || i >= len(p.kinds) || p.kinds[i] != EVM || p.limits[i] <= p.left {
		return nil
	}
	return sdkerrors.Wrapf(evmtypes.ErrBlockGasLimitReached, "gas limit %d exceeds the %d gas left in the block", p.limits[i], p.left)
}

// Use takes the gas that the transaction at index i used, as its result res reports it, out of the
// gas left. Only an EVM transaction that executed uses gas from the pool, and never more than its
// gas limit.
func (p *Pool) Use(i int, res *abci.ExecTxResult) {
	// A result without EvmTxInfo is an EVM transaction that failed before executing.
	if i < 0 || i >= len(p.kinds) || p.kinds[i] != EVM || res == nil || res.EvmTxInfo == nil || res.GasUsed <= 0 {
		return
	}
	// Capping at the gas limit keeps MayReject exact whatever unit GasUsed is reported in.
	p.left -= min(uint64(res.GasUsed), p.limits[i], p.left) //nolint:gosec // GasUsed is positive here
}

// Rejected returns the result of a transaction that Admit refused with err, which its block
// records without executing it.
func Rejected(err error) *abci.ExecTxResult {
	// EndBlock reads an EVM transaction with no deferred info and an empty log as executed, so the
	// log must not be empty.
	codespace, code, log := sdkerrors.ABCIInfo(err, false)
	return &abci.ExecTxResult{Codespace: codespace, Code: code, Log: log}
}
