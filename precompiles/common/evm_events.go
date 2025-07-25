package common

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/abi"
)

// Event signatures for staking precompile
var (
	// Delegate(address,string,uint256)
	DelegateEventSig = crypto.Keccak256Hash([]byte("Delegate(address,string,uint256)"))

	// Redelegate(address,string,string,uint256)
	RedelegateEventSig = crypto.Keccak256Hash([]byte("Redelegate(address,string,string,uint256)"))

	// Undelegate(address,string,uint256)
	UndelegateEventSig = crypto.Keccak256Hash([]byte("Undelegate(address,string,uint256)"))

	// ValidatorCreated(address,string,string)
	ValidatorCreatedEventSig = crypto.Keccak256Hash([]byte("ValidatorCreated(address,string,string)"))

	// ValidatorEdited(address,string,string)
	ValidatorEditedEventSig = crypto.Keccak256Hash([]byte("ValidatorEdited(address,string,string)"))
)

// EmitDelegateEvent emits a Delegate(address,string,uint256) event
func EmitDelegateEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	topics := []common.Hash{
		DelegateEventSig,
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

// EmitRedelegateEvent emits a Redelegate(address,string,string,uint256) event
func EmitRedelegateEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, srcValidator, dstValidator string, amount *big.Int) error {
	topics := []common.Hash{
		RedelegateEventSig,
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

// EmitUndelegateEvent emits an Undelegate(address,string,uint256) event
func EmitUndelegateEvent(evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	topics := []common.Hash{
		UndelegateEventSig,
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
