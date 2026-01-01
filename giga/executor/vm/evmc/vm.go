package evmc

import (
	"math"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/sei-protocol/sei-chain/giga/executor/types"
)

type VMImpl struct {
	hostContext evmc.HostContext
}

func NewVM(hostContext evmc.HostContext) types.VM {
	return &VMImpl{hostContext: hostContext}
}

func (v *VMImpl) Create(sender types.Address, code []byte, gas uint64, value types.Hash) (ret []byte, contractAddr types.Address, gasLeft uint64, err error) {
	if gas > math.MaxInt64 {
		panic("gas overflow")
	}
	ret, left, _, addr, err := v.hostContext.Call(evmc.Create, evmc.Address{}, evmc.Address(sender), evmc.Hash(value), code, int64(gas), 0, false, evmc.Hash{}, evmc.Address{})
	return ret, types.Address(addr), uint64(left), err //nolint:gosec
}

func (v *VMImpl) Call(sender types.Address, to types.Address, input []byte, gas uint64, value types.Hash) (ret []byte, gasLeft uint64, err error) {
	if gas > math.MaxInt64 {
		panic("gas overflow")
	}
	ret, left, _, _, err := v.hostContext.Call(evmc.Call, evmc.Address(to), evmc.Address(sender), evmc.Hash(value), input, int64(gas), 0, false, evmc.Hash{}, evmc.Address(to))
	return ret, uint64(left), err //nolint:gosec
}
