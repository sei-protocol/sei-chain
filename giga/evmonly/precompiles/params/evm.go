package params

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// The evm subspace's flat keys.
const (
	keyMaxDynamicBaseFeeUpwardAdjustment   = "KeyMaxDynamicBaseFeeUpwardAdjustment"
	keyMaxDynamicBaseFeeDownwardAdjustment = "KeyMaxDynamicBaseFeeDownwardAdjustment"
)

var evmSubspace = subspace{
	name: "evm",
	slot: slotEVM,
	params: []param{
		{key: keyMaxDynamicBaseFeeUpwardAdjustment, kind: decKind, at: evmMaxDynamicBaseFeeUpwardAdjustment},
		{key: keyMaxDynamicBaseFeeDownwardAdjustment, kind: decKind, at: evmMaxDynamicBaseFeeDownwardAdjustment},
	},
	check: checkEVM,
}

// EVM is ParamsStorage.EVM, the params that set the base fee. Each is a Dec.
type EVM struct {
	// MaxDynamicBaseFeeUpwardAdjustment is the fraction a block using its whole gas limit raises
	// the base fee by.
	MaxDynamicBaseFeeUpwardAdjustment *big.Int
	// MaxDynamicBaseFeeDownwardAdjustment is the fraction a block using no gas lowers the base fee
	// by.
	MaxDynamicBaseFeeDownwardAdjustment *big.Int
}

// ReadEVM returns the evm subspace's params. They are all zero until written.
func ReadEVM(state StorageReader) EVM {
	return evmFrom(evmSubspace.read(storageWords{state}))
}

// evmUpwardAdjustmentParam is the param HasEVM reads.
var evmUpwardAdjustmentParam = evmSubspace.mustParam(keyMaxDynamicBaseFeeUpwardAdjustment)

// HasEVM reports whether the evm subspace's params have been written.
func HasEVM(state StorageReader) bool {
	return evmSubspace.get(storageWords{state}, evmUpwardAdjustmentParam).Sign() != 0
}

var errEVMWritten = errors.New("evm's params are already written")

// WriteEVM writes e as the evm subspace's params, for genesis, if e is valid
// and they have not been written.
func WriteEVM(state precompiles.State, e EVM) error {
	if HasEVM(state) {
		return errEVMWritten
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

// checkEVM requires the params to pass EVM.Validate.
func checkEVM(v values) error {
	return evmFrom(v).Validate()
}

func evmFrom(v values) EVM {
	return EVM{
		MaxDynamicBaseFeeUpwardAdjustment:   v[keyMaxDynamicBaseFeeUpwardAdjustment],
		MaxDynamicBaseFeeDownwardAdjustment: v[keyMaxDynamicBaseFeeDownwardAdjustment],
	}
}

func (e EVM) values() values {
	return values{
		keyMaxDynamicBaseFeeUpwardAdjustment:   e.MaxDynamicBaseFeeUpwardAdjustment,
		keyMaxDynamicBaseFeeDownwardAdjustment: e.MaxDynamicBaseFeeDownwardAdjustment,
	}
}

// Validate requires an upward adjustment above 0 and a downward adjustment of at least 0, both at
// most 1.
func (e EVM) Validate() error {
	if a := e.MaxDynamicBaseFeeUpwardAdjustment; a == nil || a.Sign() <= 0 || a.Cmp(decOne) > 0 {
		return fmt.Errorf("%s must be above 0 and at most 1", keyMaxDynamicBaseFeeUpwardAdjustment)
	}
	if a := e.MaxDynamicBaseFeeDownwardAdjustment; a == nil || a.Sign() < 0 || a.Cmp(decOne) > 0 {
		return fmt.Errorf("%s must be between 0 and 1", keyMaxDynamicBaseFeeDownwardAdjustment)
	}
	return nil
}
