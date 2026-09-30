package evmonly

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	errPrecompileNegativeAmount = errors.New("custom precompile balance amount is negative")
	errPrecompileAmountOverflow = errors.New("custom precompile balance amount exceeds uint256")
)

// customPrecompile runs a native custom precompile against the calling EVM's StateDB.
type customPrecompile struct {
	address  common.Address
	contract precompiles.Contract
}

var _ vm.PrecompiledContract = customPrecompile{}

func (c customPrecompile) RequiredGas(input []byte) uint64 {
	return c.contract.RequiredGas(input)
}

// Run runs the contract against the calling EVM's StateDB. A failed state write
// takes precedence over the contract's own result.
func (c customPrecompile) Run(
	evm *vm.EVM,
	sender common.Address,
	_ common.Address,
	input []byte,
	value *big.Int,
	readOnly bool,
	isFromDelegateCall bool,
	_ *tracing.Hooks,
) ([]byte, error) {
	if !readOnly {
		materializeAccount(evm.StateDB, c.address)
	}
	state := &precompileState{db: evm.StateDB, readOnly: readOnly}
	ctx := &precompiles.Context{
		Caller:        sender,
		Address:       c.address,
		ApparentValue: cloneOptionalBig(value),
		ReadOnly:      readOnly,
		DelegateCall:  isFromDelegateCall,
		Block:         precompileBlockContext(evm),
		State:         state,
		Logs:          state,
	}
	output, err := c.contract.Run(ctx, input)
	if state.err != nil {
		return nil, state.err
	}
	// The output is kept on error so vm.ErrExecutionReverted carries revert data.
	return output, err
}

// materializeAccount gives a precompile account with no nonce and no code a nonce
// of 1. The EVM treats an account with neither and no balance as absent and
// recreates it on every call. Recreation keeps the account's committed storage,
// but it writes the account, which would make each call conflict with every
// other under OCC.
func materializeAccount(db vm.StateDB, addr common.Address) {
	if db.GetNonce(addr) == 0 && len(db.GetCode(addr)) == 0 {
		db.SetNonce(addr, 1, tracing.NonceChangeUnspecified)
	}
}

func precompileBlockContext(evm *vm.EVM) precompiles.BlockContext {
	block := precompiles.BlockContext{
		Number:      evm.Context.BlockNumber.Uint64(),
		Time:        evm.Context.Time,
		ChainID:     cloneOptionalBig(evm.ChainConfig().ChainID),
		BaseFee:     cloneOptionalBig(evm.Context.BaseFee),
		BlobBaseFee: cloneOptionalBig(evm.Context.BlobBaseFee),
		Coinbase:    evm.Context.Coinbase,
	}
	if evm.Context.Random != nil {
		block.PrevRandao = *evm.Context.Random
	}
	return block
}

// precompileState exposes the calling EVM's StateDB to a custom precompile, so
// every read and write it makes is tracked for OCC and reverted with the call.
// A write attempted under a static call is dropped and fails the call.
type precompileState struct {
	db       vm.StateDB
	readOnly bool
	err      error
}

var (
	_ precompiles.State   = (*precompileState)(nil)
	_ precompiles.LogSink = (*precompileState)(nil)
)

func (s *precompileState) GetBalance(addr common.Address) *big.Int {
	return s.db.GetBalance(addr).ToBig()
}

func (s *precompileState) AddBalance(addr common.Address, amount *big.Int) {
	value, ok := s.writableAmount(amount)
	if !ok {
		return
	}
	s.db.AddBalance(addr, value, tracing.BalanceChangeUnspecified)
}

func (s *precompileState) SubBalance(addr common.Address, amount *big.Int) error {
	value, ok := s.writableAmount(amount)
	if !ok {
		return s.err
	}
	if s.db.GetBalance(addr).Lt(value) {
		return errInsufficientBalance
	}
	s.db.SubBalance(addr, value, tracing.BalanceChangeUnspecified)
	return nil
}

func (s *precompileState) GetNonce(addr common.Address) uint64 {
	return s.db.GetNonce(addr)
}

func (s *precompileState) SetNonce(addr common.Address, nonce uint64) {
	if !s.writable() {
		return
	}
	s.db.SetNonce(addr, nonce, tracing.NonceChangeUnspecified)
}

func (s *precompileState) GetCode(addr common.Address) []byte {
	return s.db.GetCode(addr)
}

func (s *precompileState) GetState(addr common.Address, key common.Hash) common.Hash {
	return s.db.GetState(addr, key)
}

func (s *precompileState) SetState(addr common.Address, key common.Hash, value common.Hash) {
	if !s.writable() {
		return
	}
	s.db.SetState(addr, key, value)
}

func (s *precompileState) AddLog(log *ethtypes.Log) {
	if !s.writable() {
		return
	}
	s.db.AddLog(log)
}

// writable reports whether a write may proceed, recording the failure when it may not.
func (s *precompileState) writable() bool {
	if s.readOnly {
		s.fail(vm.ErrWriteProtection)
		return false
	}
	return s.err == nil
}

func (s *precompileState) writableAmount(amount *big.Int) (*uint256.Int, bool) {
	if !s.writable() {
		return nil, false
	}
	if amount.Sign() < 0 {
		s.fail(errPrecompileNegativeAmount)
		return nil, false
	}
	value, overflow := uint256.FromBig(amount)
	if overflow {
		s.fail(errPrecompileAmountOverflow)
		return nil, false
	}
	return value, true
}

func (s *precompileState) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}
