package evmonly

import (
	"errors"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	errPrecompileNegativeAmount = errors.New("custom precompile balance amount is negative")
	errPrecompileAmountOverflow = errors.New("custom precompile balance amount exceeds uint256")
)

// registeredCustomPrecompile runs a native custom precompile against the
// calling EVM's StateDB, so every read and write it makes is tracked for OCC
// conflict detection and reverted along with the call.
type registeredCustomPrecompile struct {
	address  common.Address
	contract precompiles.Contract
}

var (
	_ vm.PrecompiledContract           = registeredCustomPrecompile{}
	_ vm.DynamicGasPrecompiledContract = registeredCustomPrecompile{}
)

func (p registeredCustomPrecompile) RequiredGas(input []byte) uint64 {
	return p.contract.RequiredGas(input)
}

// Run runs the contract with unlimited gas for its storage reads.
func (p registeredCustomPrecompile) Run(
	evm *vm.EVM,
	sender common.Address,
	caller common.Address,
	input []byte,
	value *big.Int,
	readOnly bool,
	isFromDelegateCall bool,
	hooks *tracing.Hooks,
) ([]byte, error) {
	output, _, err := p.RunAndCalculateGas(evm, sender, caller, input, math.MaxUint64, value, hooks, readOnly, isFromDelegateCall)
	return output, err
}

// RunAndCalculateGas charges RequiredGas, then runs the contract against the
// calling EVM's StateDB, charging each storage read as an EIP-2929 SLOAD from
// the remaining gas. A failed state access takes precedence over the
// contract's own result so a precompile cannot return success after an invalid
// balance or storage mutation, or after running out of gas.
func (p registeredCustomPrecompile) RunAndCalculateGas(
	evm *vm.EVM,
	sender common.Address,
	_ common.Address,
	input []byte,
	suppliedGas uint64,
	value *big.Int,
	hooks *tracing.Hooks,
	readOnly bool,
	isFromDelegateCall bool,
) ([]byte, uint64, error) {
	gasCost := p.RequiredGas(input)
	if suppliedGas < gasCost {
		return nil, 0, vm.ErrOutOfGas
	}
	if hooks != nil && hooks.OnGasChange != nil {
		hooks.OnGasChange(suppliedGas, suppliedGas-gasCost, tracing.GasChangeCallPrecompiledContract)
	}
	if !readOnly {
		materializeAccount(evm.StateDB, p.address)
	}
	start := suppliedGas - gasCost
	state := &precompileState{db: evm.StateDB, readOnly: readOnly, metered: true, gas: start}
	ctx := &precompiles.Context{
		Caller:        sender,
		Address:       p.address,
		ApparentValue: cloneOptionalBig(value),
		ReadOnly:      readOnly,
		DelegateCall:  isFromDelegateCall,
		GasRemaining:  start,
		Block:         precompileBlockContext(evm),
		State:         state,
		Logs:          state,
	}
	output, err := p.contract.Run(ctx, input)
	if state.err != nil {
		state.gas = 0
	}
	if hooks != nil && hooks.OnGasChange != nil && state.gas != start {
		hooks.OnGasChange(start, state.gas, tracing.GasChangeCallPrecompiledContract)
	}
	if state.err != nil {
		return nil, 0, state.err
	}
	// The output is kept on error so vm.ErrExecutionReverted carries revert data.
	return output, state.gas, err
}

// materializeAccount gives a precompile account with no nonce and no code a
// nonce of 1.
func materializeAccount(db vm.StateDB, addr common.Address) {
	// EIP-158 treats an account with no nonce, code or balance as absent, and
	// evm.Call would recreate it on every call: an account-level write that
	// makes every call to the precompile conflict under OCC.
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
// A write attempted under a static call is dropped and fails the call. When
// metered, storage reads are paid from gas and fail the call once it runs out.
type precompileState struct {
	db       vm.StateDB
	readOnly bool
	metered  bool
	gas      uint64
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
		s.fail(errInsufficientPrecompileBalance)
		return errInsufficientPrecompileBalance
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
	if !s.chargeSload(addr, key) {
		return common.Hash{}
	}
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

var errInsufficientPrecompileBalance = errors.New("custom precompile balance is insufficient")

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
	if amount == nil {
		return uint256.NewInt(0), true
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

// chargeSload charges an EIP-2929 SLOAD of key and warms it, recording
// vm.ErrOutOfGas when the gas left does not cover it.
func (s *precompileState) chargeSload(addr common.Address, key common.Hash) bool {
	if !s.metered {
		return true
	}
	if s.err != nil {
		return false
	}
	cost := params.WarmStorageReadCostEIP2929
	if _, warm := s.db.SlotInAccessList(addr, key); !warm {
		cost = params.ColdSloadCostEIP2929
	}
	if s.gas < cost {
		s.fail(vm.ErrOutOfGas)
		return false
	}
	s.gas -= cost
	s.db.AddSlotToAccessList(addr, key)
	return true
}

func (s *precompileState) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}
