package common

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

// EmitEVMLog emits an EVM log from a precompile
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
		// BlockNumber, BlockHash, TxHash, TxIndex, and Index are added later
		// by the consensus engine when the block is being finalized.
	})
	return nil
}

// Event signatures for staking precompile
var (
	// Delegate(address indexed delegator, string validator, uint256 amount)
	DelegateEventSig = crypto.Keccak256Hash([]byte("Delegate(address,string,uint256)"))

	// Redelegate(address indexed delegator, string srcValidator, string dstValidator, uint256 amount)
	RedelegateEventSig = crypto.Keccak256Hash([]byte("Redelegate(address,string,string,uint256)"))

	// Undelegate(address indexed delegator, string validator, uint256 amount)
	UndelegateEventSig = crypto.Keccak256Hash([]byte("Undelegate(address,string,uint256)"))

	// ValidatorCreated(address indexed validator, string validatorAddress, string moniker)
	ValidatorCreatedEventSig = crypto.Keccak256Hash([]byte("ValidatorCreated(address,string,string)"))

	// ValidatorEdited(address indexed validator, string validatorAddress, string moniker)
	ValidatorEditedEventSig = crypto.Keccak256Hash([]byte("ValidatorEdited(address,string,string)"))
)

// Helper functions for common event patterns
func EmitDelegateEvent(ctx sdk.Context, evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	// Pack the non-indexed data: validator string and amount
	// For strings in events, we need to encode: offset, length, and actual string data
	data := make([]byte, 0)

	// Offset for string (always 64 for first dynamic param when second param is uint256)
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)

	// Amount (uint256)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)

	// String length
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(validator))).Bytes(), 32)...)

	// String data (padded to 32 bytes)
	valBytes := []byte(validator)
	data = append(data, common.RightPadBytes(valBytes, ((len(valBytes)+31)/32)*32)...)

	topics := []common.Hash{
		DelegateEventSig,
		common.BytesToHash(delegator.Bytes()), // indexed
	}

	return EmitEVMLog(evm, precompileAddr, topics, data)
}

func EmitRedelegateEvent(ctx sdk.Context, evm *vm.EVM, precompileAddr common.Address, delegator common.Address, srcValidator, dstValidator string, amount *big.Int) error {
	// Pack the non-indexed data: srcValidator, dstValidator, amount
	// - dstValidator string data (padded to 32 bytes)
	var data []byte
	// offset for srcValidator
	// The static part consists of 3 items (2 offsets and amount), so 3 * 32 = 96 bytes.
	// The dynamic data for srcValidator starts at offset 96.
	data = append(data, common.LeftPadBytes(big.NewInt(96).Bytes(), 32)...)
	// offset for dstValidator
	// The data for srcValidator consists of its length (32 bytes) and data (padded to 32 bytes), so 64 bytes total.
	// The dynamic data for dstValidator starts after srcValidator's data, so at offset 96 + 64 = 160.
	data = append(data, common.LeftPadBytes(big.NewInt(160).Bytes(), 32)...)
	// amount
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	// length of srcValidator
	srcBytes := []byte(srcValidator)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(srcBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(srcBytes, ((len(srcBytes)+31)/32)*32)...)

	// dstValidator string
	dstBytes := []byte(dstValidator)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(dstBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(dstBytes, ((len(dstBytes)+31)/32)*32)...)

	topics := []common.Hash{
		RedelegateEventSig,
		common.BytesToHash(delegator.Bytes()), // indexed
	}

	return EmitEVMLog(evm, precompileAddr, topics, data)
}

func EmitUndelegateEvent(ctx sdk.Context, evm *vm.EVM, precompileAddr common.Address, delegator common.Address, validator string, amount *big.Int) error {
	// Pack the non-indexed data: validator string and amount
	data := make([]byte, 0)

	// Offset for string
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)

	// Amount
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)

	// String length and data
	valBytes := []byte(validator)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(valBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(valBytes, ((len(valBytes)+31)/32)*32)...)

	topics := []common.Hash{
		UndelegateEventSig,
		common.BytesToHash(delegator.Bytes()), // indexed
	}

	return EmitEVMLog(evm, precompileAddr, topics, data)
}

func EmitValidatorCreatedEvent(ctx sdk.Context, evm *vm.EVM, precompileAddr common.Address, creator common.Address, validatorAddr string, moniker string) error {
	// Pack the non-indexed data: validatorAddr string and moniker string
	data := make([]byte, 0)

	// Offsets for two strings
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)  // offset for validatorAddr
	data = append(data, common.LeftPadBytes(big.NewInt(128).Bytes(), 32)...) // offset for moniker (approximate)

	// validatorAddr string
	valAddrBytes := []byte(validatorAddr)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(valAddrBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(valAddrBytes, ((len(valAddrBytes)+31)/32)*32)...)

	// Adjust offset for moniker based on actual validatorAddr length
	monikerOffset := 64 + 32 + ((len(valAddrBytes)+31)/32)*32
	// Update the moniker offset in data
	copy(data[32:64], common.LeftPadBytes(big.NewInt(int64(monikerOffset)).Bytes(), 32))

	// moniker string
	monikerBytes := []byte(moniker)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(monikerBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(monikerBytes, ((len(monikerBytes)+31)/32)*32)...)

	topics := []common.Hash{
		ValidatorCreatedEventSig,
		common.BytesToHash(creator.Bytes()), // indexed
	}

	return EmitEVMLog(evm, precompileAddr, topics, data)
}

func EmitValidatorEditedEvent(ctx sdk.Context, evm *vm.EVM, precompileAddr common.Address, editor common.Address, validatorAddr string, moniker string) error {
	// Pack the non-indexed data: validatorAddr string and moniker string
	data := make([]byte, 0)

	// Offsets for two strings
	data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)  // offset for validatorAddr
	data = append(data, common.LeftPadBytes(big.NewInt(128).Bytes(), 32)...) // offset for moniker (approximate)

	// validatorAddr string
	valAddrBytes := []byte(validatorAddr)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(valAddrBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(valAddrBytes, ((len(valAddrBytes)+31)/32)*32)...)

	// Adjust offset for moniker based on actual validatorAddr length
	monikerOffset := 64 + 32 + ((len(valAddrBytes)+31)/32)*32
	// Update the moniker offset in data
	copy(data[32:64], common.LeftPadBytes(big.NewInt(int64(monikerOffset)).Bytes(), 32))

	// moniker string
	monikerBytes := []byte(moniker)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(len(monikerBytes))).Bytes(), 32)...)
	data = append(data, common.RightPadBytes(monikerBytes, ((len(monikerBytes)+31)/32)*32)...)

	topics := []common.Hash{
		ValidatorEditedEventSig,
		common.BytesToHash(editor.Bytes()), // indexed
	}

	return EmitEVMLog(evm, precompileAddr, topics, data)
}
