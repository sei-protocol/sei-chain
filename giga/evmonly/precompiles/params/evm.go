package params

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// The evm subspace's flat keys.
const (
	keyMinFeePerGas             = "KeyMinFeePerGas"
	keyTargetGasUsedPerSecond   = "KeyTargetGasUsedPerSecond"
	keyBaseFeeChangeDenominator = "KeyBaseFeeChangeDenominator"
)

var evmSubspace = subspace{
	name: "evm",
	slot: slotEVM,
	params: []param{
		{key: keyMinFeePerGas, kind: decKind, at: evmMinFeePerGas},
		{key: keyTargetGasUsedPerSecond, kind: uint64Kind, at: evmTargetGasUsedPerSecond},
		{key: keyBaseFeeChangeDenominator, kind: uint64Kind, at: evmBaseFeeChangeDenominator},
	},
	check: checkEVM,
}

// EVM is ParamsStorage.EVM, the params that set the base fee.
type EVM struct {
	// MinFeePerGas is the lowest base fee, in wei per gas. It is stored as a
	// Dec holding a whole number of wei.
	MinFeePerGas *big.Int
	// TargetGasUsedPerSecond is the rate of gas use that leaves the base fee
	// unchanged.
	TargetGasUsedPerSecond uint64
	// BaseFeeChangeDenominator sets the base fee's step size: a block using
	// twice its target raises it by 1/BaseFeeChangeDenominator, and an empty
	// block lowers it by as much.
	BaseFeeChangeDenominator uint64
}

// ReadEVM returns the evm subspace's params. They are all zero until written.
func ReadEVM(state StorageReader) EVM {
	return evmFrom(evmSubspace.read(storageWords{state}))
}

// evmDenominatorParam is the param HasEVM reads.
var evmDenominatorParam = evmSubspace.mustParam(keyBaseFeeChangeDenominator)

// HasEVM reports whether the evm subspace's params have been written.
func HasEVM(state StorageReader) bool {
	return evmSubspace.get(storageWords{state}, evmDenominatorParam).Sign() != 0
}

var errEVMWritten = errors.New("evm's params are already written")

// WriteEVM writes e as the evm subspace's params, for genesis, if e is valid
// and they have not been written.
func WriteEVM(state precompiles.State, e EVM) error {
	if HasEVM(state) {
		return errEVMWritten
	}
	if err := e.Validate(); err != nil { // values reads a nil MinFeePerGas as zero
		return err
	}
	v := e.values()
	if err := evmSubspace.validate(v); err != nil {
		return err
	}
	s := newStore(state)
	for _, p := range evmSubspace.params {
		evmSubspace.set(s, p, v[p.key])
	}
	return nil
}

// checkEVM requires KeyMinFeePerGas to be a whole number of wei and the params
// to pass EVM.Validate.
func checkEVM(v values) error {
	if new(big.Int).Mod(v[keyMinFeePerGas], decOne).Sign() != 0 {
		return fmt.Errorf("%s must be a whole number of wei", keyMinFeePerGas)
	}
	return evmFrom(v).Validate()
}

func evmFrom(v values) EVM {
	return EVM{
		MinFeePerGas:             new(big.Int).Quo(v[keyMinFeePerGas], decOne),
		TargetGasUsedPerSecond:   v[keyTargetGasUsedPerSecond].Uint64(),
		BaseFeeChangeDenominator: v[keyBaseFeeChangeDenominator].Uint64(),
	}
}

func (e EVM) values() values {
	minFeePerGas := new(big.Int)
	if e.MinFeePerGas != nil {
		minFeePerGas.Mul(e.MinFeePerGas, decOne)
	}
	return values{
		keyMinFeePerGas:             minFeePerGas,
		keyTargetGasUsedPerSecond:   new(big.Int).SetUint64(e.TargetGasUsedPerSecond),
		keyBaseFeeChangeDenominator: new(big.Int).SetUint64(e.BaseFeeChangeDenominator),
	}
}

// Validate requires a MinFeePerGas that is not negative, and a positive
// TargetGasUsedPerSecond and BaseFeeChangeDenominator.
func (e EVM) Validate() error {
	if e.MinFeePerGas == nil || e.MinFeePerGas.Sign() < 0 {
		return fmt.Errorf("%s must not be negative", keyMinFeePerGas)
	}
	if e.TargetGasUsedPerSecond == 0 {
		return fmt.Errorf("%s must be positive", keyTargetGasUsedPerSecond)
	}
	if e.BaseFeeChangeDenominator == 0 {
		return fmt.Errorf("%s must be positive", keyBaseFeeChangeDenominator)
	}
	return nil
}
