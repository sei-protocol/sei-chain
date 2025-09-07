package common

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

// EmitEVMLog emits a log from the EVM using Sei's underlying state implementation.
func EmitEVMLog(evm *vm.EVM, address common.Address, topics []common.Hash, data []byte) error {
	if len(topics) > 4 {
		return errors.New("log topics cannot be more than 4")
	}
	if evm == nil {
		return fmt.Errorf("EVM is nil")
	}
	if evm.StateDB == nil {
		return fmt.Errorf("EVM StateDB is nil")
	}

	stateDB := state.GetDBImpl(evm.StateDB)
	if stateDB == nil {
		return fmt.Errorf("cannot emit log: invalid StateDB type")
	}

	stateDB.AddLog(&ethtypes.Log{
		Address: address,
		Topics:  topics,
		Data:    data,
	})
	return nil
}

// Event signatures
var (
	DelegateEventSig           = crypto.Keccak256Hash([]byte("Delegate(address,string,uint256)"))
	RedelegateEventSig         = crypto.Keccak256Hash([]byte("Redelegate(address,string,string,uint256)"))
	UndelegateEventSig         = crypto.Keccak256Hash([]byte("Undelegate(address,string,uint256)"))
	ValidatorCreatedEventSig   = crypto.Keccak256Hash([]byte("ValidatorCreated(address,string,string)"))
	ValidatorEditedEventSig    = crypto.Keccak256Hash([]byte("ValidatorEdited(address,string,string)"))
	CancelUndelegationEventSig = crypto.Keccak256Hash([]byte("CancelUndelegation(address,string,uint256)"))
)

// ---- Delegate ----
func BuildDelegateEvent(delegator common.Address, validator string, amount *big.Int) ([]common.Hash, []byte, error) {
	data := make([]byte, 0)
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(validator))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes([]byte(validator), ((len(validator)+31)/32)*32)...)

	topics := []common.Hash{
		DelegateEventSig,
		common.BytesToHash(delegator.Bytes()),
	}
	return topics, data, nil
}

func EmitDelegateEvent(evm *vm.EVM, precompileAddr, delegator common.Address, validator string, amount *big.Int) error {
	topics, data, err := BuildDelegateEvent(delegator, validator, amount)
	if err != nil {
		return err
	}
	return EmitEVMLog(evm, precompileAddr, topics, data)
}

// ---- Undelegate ----
func BuildUndelegateEvent(delegator common.Address, validator string, amount *big.Int) ([]common.Hash, []byte, error) {
	data := make([]byte, 0)
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(validator))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes([]byte(validator), ((len(validator)+31)/32)*32)...)

	topics := []common.Hash{
		UndelegateEventSig,
		common.BytesToHash(delegator.Bytes()),
	}
	return topics, data, nil
}

func EmitUndelegateEvent(evm *vm.EVM, precompileAddr, delegator common.Address, validator string, amount *big.Int) error {
	topics, data, err := BuildUndelegateEvent(delegator, validator, amount)
	if err != nil {
		return err
	}
	return EmitEVMLog(evm, precompileAddr, topics, data)
}

// ---- Cancel Undelegation ----
func BuildCancelUndelegationEvent(delegator common.Address, validator string, amount *big.Int) ([]common.Hash, []byte, error) {
	data := make([]byte, 0)
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(validator))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes([]byte(validator), ((len(validator)+31)/32)*32)...)

	topics := []common.Hash{
		CancelUndelegationEventSig,
		common.BytesToHash(delegator.Bytes()),
	}
	return topics, data, nil
}

func EmitCancelUndelegationEvent(evm *vm.EVM, precompileAddr, delegator common.Address, validator string, amount *big.Int) error {
	topics, data, err := BuildCancelUndelegationEvent(delegator, validator, amount)
	if err != nil {
		return err
	}
	return EmitEVMLog(evm, precompileAddr, topics, data)
}

// ---- Redelegate ----
func BuildRedelegateEvent(delegator common.Address, srcValidator, dstValidator string, amount *big.Int) ([]common.Hash, []byte, error) {
	data := make([]byte, 0)

	// Offset for srcValidator (after 3 static values)
	data = append(data, common.LeftPadBytes(big.NewInt(96).Bytes(), 32)...)
	// Placeholder offset for dstValidator, filled after src padding
	data = append(data, common.LeftPadBytes(big.NewInt(0).Bytes(), 32)...)
	// Amount
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)

	// srcValidator
	srcBytes := []byte(srcValidator)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(srcBytes))).Bytes(), 32)...)
	paddedSrc := common.RightPadBytes(srcBytes, ((len(srcBytes)+31)/32)*32)
	data = append(data, paddedSrc...)

	// Calculate dstValidator offset
	dstOffset := 96 + 32 + len(paddedSrc)
	copy(data[32:64], common.LeftPadBytes(big.NewInt(int64(dstOffset)).Bytes(), 32))

	// dstValidator
	dstBytes := []byte(dstValidator)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(dstBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(dstBytes, ((len(dstBytes)+31)/32)*32)...)

	topics := []common.Hash{
		RedelegateEventSig,
		common.BytesToHash(delegator.Bytes()),
	}
	return topics, data, nil
}

func EmitRedelegateEvent(evm *vm.EVM, precompileAddr, delegator common.Address, srcValidator, dstValidator string, amount *big.Int) error {
	topics, data, err := BuildRedelegateEvent(delegator, srcValidator, dstValidator, amount)
	if err != nil {
		return err
	}
	return EmitEVMLog(evm, precompileAddr, topics, data)
}

// ---- Validator Created ----
func BuildValidatorCreatedEvent(creator common.Address, validatorAddr, moniker string) ([]common.Hash, []byte, error) {
	data := make([]byte, 0)

	// Offsets
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(128).Bytes(), 32)...)

	// Validator address
	valBytes := []byte(validatorAddr)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(valBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(valBytes, ((len(valBytes)+31)/32)*32)...)

	// Adjust offset for moniker
	monikerOffset := 64 + 32 + ((len(valBytes)+31)/32)*32
	copy(data[32:64], common.LeftPadBytes(big.NewInt(int64(monikerOffset)).Bytes(), 32))

	// Moniker
	monikerBytes := []byte(moniker)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(monikerBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(monikerBytes, ((len(monikerBytes)+31)/32)*32)...)

	topics := []common.Hash{
		ValidatorCreatedEventSig,
		common.BytesToHash(creator.Bytes()),
	}
	return topics, data, nil
}

func EmitValidatorCreatedEvent(evm *vm.EVM, precompileAddr, creator common.Address, validatorAddr, moniker string) error {
	topics, data, err := BuildValidatorCreatedEvent(creator, validatorAddr, moniker)
	if err != nil {
		return err
	}
	return EmitEVMLog(evm, precompileAddr, topics, data)
}

// ---- Validator Edited ----
func BuildValidatorEditedEvent(editor common.Address, validatorAddr, moniker string) ([]common.Hash, []byte, error) {
	return BuildValidatorCreatedEvent(editor, validatorAddr, moniker) // same structure
}

func EmitValidatorEditedEvent(evm *vm.EVM, precompileAddr, editor common.Address, validatorAddr, moniker string) error {
	topics, data, err := BuildValidatorEditedEvent(editor, validatorAddr, moniker)
	if err != nil {
		return err
	}
	return EmitEVMLog(evm, precompileAddr, topics, data)
}
