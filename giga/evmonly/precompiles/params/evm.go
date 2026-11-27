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
	keyMinFeePerGas                        = "KeyMinFeePerGas"
	keyBaseFeeLag                          = "KeyBaseFeeLag"
)

// MinBaseFeeDownwardAdjustment is the smallest KeyMaxDynamicBaseFeeDownwardAdjustment, 0.01% as a
// Dec: enough for empty blocks to bring the base fee down from a spike in hours rather than days,
// since a thousandfold fall takes about 69,000 of them, under eight hours at 400 ms blocks.
const MinBaseFeeDownwardAdjustment uint64 = 100_000_000_000_000

// Bounds on KeyBaseFeeLag, in blocks.
const (
	MinBaseFeeLag uint32 = 1
	MaxBaseFeeLag uint32 = 16
)

var evmSubspace = subspace{
	name: "evm",
	slot: slotEVM,
	params: []param{
		{key: keyMaxDynamicBaseFeeUpwardAdjustment, kind: decKind, at: evmMaxDynamicBaseFeeUpwardAdjustment},
		{key: keyMaxDynamicBaseFeeDownwardAdjustment, kind: decKind, at: evmMaxDynamicBaseFeeDownwardAdjustment, least: minBaseFeeDownwardAdjustment},
		{key: keyMinFeePerGas, kind: decKind, at: evmMinFeePerGas},
		{key: keyBaseFeeLag, kind: uint32Kind, at: evmBaseFeeLag},
	},
	check: checkEVM,
}

// EVM is ParamsStorage.EVM, the params that set the base fee.
type EVM struct {
	// MaxDynamicBaseFeeUpwardAdjustment is the fraction a block using its whole gas limit raises
	// the base fee by, as a Dec.
	MaxDynamicBaseFeeUpwardAdjustment *big.Int
	// MaxDynamicBaseFeeDownwardAdjustment is the fraction a block using no gas lowers the base fee
	// by, as a Dec.
	MaxDynamicBaseFeeDownwardAdjustment *big.Int
	// MinFeePerGas is the lowest base fee, in wei per gas, as a Dec.
	MinFeePerGas *big.Int
	// BaseFeeLag is how many blocks after a block its gas use sets the base fee of.
	BaseFeeLag uint32
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
		MinFeePerGas:                        v[keyMinFeePerGas],
		BaseFeeLag:                          uint32(v[keyBaseFeeLag].Uint64()), //nolint:gosec // validate bounds it to 32 bits
	}
}

func (e EVM) values() values {
	return values{
		keyMaxDynamicBaseFeeUpwardAdjustment:   e.MaxDynamicBaseFeeUpwardAdjustment,
		keyMaxDynamicBaseFeeDownwardAdjustment: e.MaxDynamicBaseFeeDownwardAdjustment,
		keyMinFeePerGas:                        e.MinFeePerGas,
		keyBaseFeeLag:                          new(big.Int).SetUint64(uint64(e.BaseFeeLag)),
	}
}

// Validate requires an upward adjustment above 0 and a downward adjustment of at least
// MinBaseFeeDownwardAdjustment, both at most 1, a minimum fee per gas of a whole number of wei,
// and a lag of MinBaseFeeLag to MaxBaseFeeLag blocks.
func (e EVM) Validate() error {
	if a := e.MaxDynamicBaseFeeUpwardAdjustment; a == nil || a.Sign() <= 0 || a.Cmp(decOne) > 0 {
		return fmt.Errorf("%s must be above 0 and at most 1", keyMaxDynamicBaseFeeUpwardAdjustment)
	}
	if a := e.MaxDynamicBaseFeeDownwardAdjustment; a == nil || a.Cmp(minBaseFeeDownwardAdjustment) < 0 || a.Cmp(decOne) > 0 {
		return fmt.Errorf("%s must be between 0.0001 and 1", keyMaxDynamicBaseFeeDownwardAdjustment)
	}
	if m := e.MinFeePerGas; m == nil || m.Sign() < 0 || new(big.Int).Mod(m, decOne).Sign() != 0 {
		return fmt.Errorf("%s must be a whole number of wei", keyMinFeePerGas)
	}
	if e.BaseFeeLag < MinBaseFeeLag || e.BaseFeeLag > MaxBaseFeeLag {
		return fmt.Errorf("%s must be in %d..%d blocks", keyBaseFeeLag, MinBaseFeeLag, MaxBaseFeeLag)
	}
	return nil
}

var minBaseFeeDownwardAdjustment = new(big.Int).SetUint64(MinBaseFeeDownwardAdjustment)
