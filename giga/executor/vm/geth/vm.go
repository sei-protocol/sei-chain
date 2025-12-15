package geth

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/giga/executor/types"
)

var _ types.VM = &VMImpl{}

type VMImpl struct {
	evm *vm.EVM
}

func NewVM(evm *vm.EVM) types.VM {
	return &VMImpl{evm: evm}
}

func (v *VMImpl) Create(sender types.Address, code []byte, gas uint64, value types.Hash) (ret []byte, contractAddr types.Address, gasLeft uint64, err error) {
	ret, addr, gasLeft, err := v.evm.Create(common.Address(sender), code, gas, new(uint256.Int).SetBytes(value[:]))
	return ret, types.Address(addr), gasLeft, err
}

func (v *VMImpl) Call(sender types.Address, to types.Address, input []byte, gas uint64, value types.Hash) (ret []byte, gasLeft uint64, err error) {
	ret, gasLeft, err = v.evm.Call(common.Address(sender), common.Address(to), input, gas, new(uint256.Int).SetBytes(value[:]))
	return ret, gasLeft, err
}
