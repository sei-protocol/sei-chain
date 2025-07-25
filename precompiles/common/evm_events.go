package common

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/abi"
)

// EmitDelegateEvent emits a Delegate(address,string,uint256) event
func EmitDelegateEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("Delegate(address,string,uint256)")),
		common.BytesToHash(delegator.Bytes()),
		crypto.Keccak256Hash([]byte(validator)),
	}
	data, err := abi.U256(amount)
	if err != nil {
		return err
	}
	evm.StateDB.AddLog(&types.Log{
		Address: precompileAddr,
		Topics:  topics,
		Data:    data,
	})
	return nil
}

// EmitUndelegateEvent emits an Undelegate(address,string,uint256) event
func EmitUndelegateEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("Undelegate(address,string,uint256)")),
		common.BytesToHash(delegator.Bytes()),
		crypto.Keccak256Hash([]byte(validator)),
	}
	data, err := abi.U256(amount)
	if err != nil {
		return err
	}
	evm.StateDB.AddLog(&types.Log{
		Address: precompileAddr,
		Topics:  topics,
		Data:    data,
	})
	return nil
}

// EmitBeginRedelegateEvent emits a BeginRedelegate(address,string,string,uint256) event
func EmitBeginRedelegateEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, srcValidator string, dstValidator string, amount *big.Int) error {
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("BeginRedelegate(address,string,string,uint256)")),
		common.BytesToHash(delegator.Bytes()),
		crypto.Keccak256Hash([]byte(srcValidator)),
		crypto.Keccak256Hash([]byte(dstValidator)),
	}
	data, err := abi.U256(amount)
	if err != nil {
		return err
	}
	evm.StateDB.AddLog(&types.Log{
		Address: precompileAddr,
		Topics:  topics,
		Data:    data,
	})
	return nil
}

// EmitCancelUndelegationEvent emits a CancelUndelegation(address,string,uint256) event
func EmitCancelUndelegationEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	topics := []common.Hash{
		crypto.Keccak256Hash([]byte("CancelUndelegation(address,string,uint256)")),
		common.BytesToHash(delegator.Bytes()),
		crypto.Keccak256Hash([]byte(validator)),
	}
	data, err := abi.U256(amount)
	if err != nil {
		return err
	}
	evm.StateDB.AddLog(&types.Log{
		Address: precompileAddr,
		Topics:  topics,
		Data:    data,
	})
	return nil
}
