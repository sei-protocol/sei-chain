package internal

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/giga/executor/precompiles"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

var _ evmc.HostContext = (*HostContext)(nil)

type HostContext struct {
	vm  *evmc.VM
	evm *vm.EVM
}

func NewHostContext(vm *evmc.VM, evm *vm.EVM) *HostContext {
	return &HostContext{vm: vm, evm: evm}
}

func (h *HostContext) AccountExists(addr evmc.Address) bool {
	return h.evm.StateDB.Exist(common.Address(addr))
}

func (h *HostContext) GetStorage(addr evmc.Address, key evmc.Hash) evmc.Hash {
	return evmc.Hash(h.evm.StateDB.GetState(common.Address(addr), common.Hash(key)))
}

func (h *HostContext) SetStorage(addr evmc.Address, key evmc.Hash, value evmc.Hash) evmc.StorageStatus {
	gethAddr := common.Address(addr)
	gethKey := common.Hash(key)

	current := h.evm.StateDB.GetState(gethAddr, gethKey)
	original := h.evm.StateDB.GetCommittedState(gethAddr, gethKey)

	dirty := original.Cmp(current) != 0
	restored := original.Cmp(common.Hash(value)) == 0
	currentIsZero := current.Cmp(common.Hash{}) == 0
	valueIsZero := common.Hash(value).Cmp(common.Hash{}) == 0

	status := evmc.StorageAssigned
	if !dirty && !restored {
		if currentIsZero {
			status = evmc.StorageAdded
		} else if valueIsZero {
			status = evmc.StorageDeleted
		} else {
			status = evmc.StorageModified
		}
	} else if dirty && !restored {
		if currentIsZero && valueIsZero {
			status = evmc.StorageDeletedAdded
		} else if !currentIsZero && valueIsZero {
			status = evmc.StorageModifiedDeleted
		}
	} else if dirty {
		if currentIsZero {
			status = evmc.StorageDeletedRestored
		} else if valueIsZero {
			status = evmc.StorageAddedDeleted
		} else {
			status = evmc.StorageModifiedRestored
		}
	}

	h.evm.StateDB.SetState(gethAddr, gethKey, common.Hash(value))
	return status
}

func (h *HostContext) GetBalance(addr evmc.Address) evmc.Hash {
	return h.evm.StateDB.GetBalance(common.Address(addr)).Bytes32()
}

func (h *HostContext) GetCodeSize(addr evmc.Address) int {
	return h.evm.StateDB.GetCodeSize(common.Address(addr))
}

func (h *HostContext) GetCodeHash(addr evmc.Address) evmc.Hash {
	return evmc.Hash(h.evm.StateDB.GetCodeHash(common.Address(addr)))
}

func (h *HostContext) GetCode(addr evmc.Address) []byte {
	return h.evm.StateDB.GetCode(common.Address(addr))
}

// todo(pdrobnjak): support historical selfdestruct logic as well
func (h *HostContext) Selfdestruct(addr evmc.Address, beneficiary evmc.Address) bool {
	addrKey := common.Address(addr)
	beneficiaryKey := common.Address(beneficiary)
	amt := h.evm.StateDB.GetBalance(addrKey)
	h.evm.StateDB.SubBalance(addrKey, amt, tracing.BalanceDecreaseSelfdestruct)
	h.evm.StateDB.AddBalance(beneficiaryKey, amt, tracing.BalanceIncreaseSelfdestruct)
	h.evm.StateDB.SelfDestruct6780(common.Address(addr))
	return true
}

func (h *HostContext) GetTxContext() evmc.TxContext {
	var gasPrice evmc.Hash
	h.evm.GasPrice.FillBytes(gasPrice[:])

	var prevRandao evmc.Hash
	if h.evm.Context.Random != nil {
		prevRandao = evmc.Hash(*h.evm.Context.Random)
	}

	var chainID evmc.Hash
	h.evm.ChainConfig().ChainID.FillBytes(chainID[:])

	var baseFee evmc.Hash
	h.evm.Context.BaseFee.FillBytes(baseFee[:])

	var blobBaseFee evmc.Hash
	h.evm.Context.BlobBaseFee.FillBytes(blobBaseFee[:])

	//nolint:gosec // G115: safe integer conversions for Time and GasLimit
	return evmc.TxContext{
		GasPrice:    gasPrice,
		Origin:      evmc.Address(h.evm.Origin),
		Coinbase:    evmc.Address(h.evm.Context.Coinbase),
		Number:      h.evm.Context.BlockNumber.Int64(),
		Timestamp:   int64(h.evm.Context.Time),
		GasLimit:    int64(h.evm.Context.GasLimit),
		PrevRandao:  prevRandao,
		ChainID:     chainID,
		BaseFee:     baseFee,
		BlobBaseFee: blobBaseFee,
	}
}

func (h *HostContext) GetBlockHash(number int64) evmc.Hash {
	//nolint:gosec // G115: safe, block numbers are always positive
	return evmc.Hash(h.evm.Context.GetHash(uint64(number)))
}

func (h *HostContext) EmitLog(addr evmc.Address, topics []evmc.Hash, data []byte) {
	gethTopics := make([]common.Hash, len(topics))
	for i, topic := range topics {
		gethTopics[i] = common.Hash(topic)
	}
	h.evm.StateDB.AddLog(&ethtypes.Log{Address: common.Address(addr), Topics: gethTopics, Data: data})
}

func (h *HostContext) Execute(kind evmc.CallKind, recipient evmc.Address, sender evmc.Address, value evmc.Hash, input []byte, gas int64,
	depth int, static bool) ([]byte, int64, int64, evmc.Address, error) {
	evmRevision := h.getEVMRevision()
	delegated := kind == evmc.DelegateCall || kind == evmc.CallCode
	code := h.evm.StateDB.GetCode(common.Address(recipient))

	executionResult, err := h.vm.Execute(
		h, evmRevision, kind, static, delegated, depth,
		gas, recipient, sender, input, value, code,
	)
	if err != nil {
		return nil, 0, 0, evmc.Address{}, err
	}

	// todo(pdrobnjak): calculate/propagate created address
	var createAddr evmc.Address
	if kind == evmc.Create || kind == evmc.Create2 {
		// The created address should be set in the execution result
		// For now, return empty - this needs to be populated from evmone's result
		createAddr = evmc.Address{}
	}

	return executionResult.Output, executionResult.GasLeft, executionResult.GasRefund, createAddr, nil
}

func (h *HostContext) Call(
	kind evmc.CallKind, recipient evmc.Address, sender evmc.Address, value evmc.Hash, input []byte, gas int64,
	_ int, static bool, salt evmc.Hash, _ evmc.Address,
) ([]byte, int64, int64, evmc.Address, error) {
	recipientAddr := common.Address(recipient)
	senderAddr := common.Address(sender)
	valueUint256 := new(uint256.Int).SetBytes(value[:])
	var ret []byte
	var leftoverGas uint64
	var err error
	var createAddr common.Address

	//nolint:gosec // G115: safe integer conversions for gas values
	switch kind {
	case evmc.Call:
		if static {
			ret, leftoverGas, err = h.evm.StaticCall(senderAddr, recipientAddr, input, uint64(gas))
		} else {
			ret, leftoverGas, err = h.evm.Call(senderAddr, recipientAddr, input, uint64(gas), valueUint256)
		}
	case evmc.DelegateCall:
		// todo(pdrobnjak): sender and recipient might not be correctly propagated in case of DELEGATECALL
		ret, leftoverGas, err = h.evm.DelegateCall(
			h.evm.Origin, senderAddr, recipientAddr, input, uint64(gas), valueUint256,
		)
	case evmc.CallCode:
		ret, leftoverGas, err = h.evm.CallCode(senderAddr, recipientAddr, input, uint64(gas), valueUint256)
	case evmc.Create:
		ret, createAddr, leftoverGas, err = h.evm.Create(senderAddr, input, uint64(gas), valueUint256)
		return ret, int64(leftoverGas), 0, evmc.Address(createAddr), err
	case evmc.Create2:
		saltUint256 := new(uint256.Int).SetBytes(salt[:])
		ret, createAddr, leftoverGas, err = h.evm.Create2(senderAddr, input, uint64(gas), valueUint256, saltUint256)
		return ret, int64(leftoverGas), 0, evmc.Address(createAddr), err
	default:
		panic("EofCreate is not supported")
	}

	//nolint:gosec // G115: safe, leftoverGas won't exceed int64 max
	return ret, int64(leftoverGas), 0, evmc.Address{}, err
}

func (h *HostContext) AccessAccount(addr evmc.Address) evmc.AccessStatus {
	addrInAccessList := h.evm.StateDB.AddressInAccessList(common.Address(addr))
	if addrInAccessList {
		return evmc.WarmAccess
	}
	// todo(pdrobnjak): poll something similar to - https://github.com/sei-protocol/sei-v3/blob/cd50388d4d423501b15a544612643073680aa8de/execute/store/types/types.go#L23 - temporarily we can expose access via our statedb impl for testing
	return evmc.ColdAccess
}

func (h *HostContext) AccessStorage(addr evmc.Address, key evmc.Hash) evmc.AccessStatus {
	addrInAccessList, slotInAccessList := h.evm.StateDB.SlotInAccessList(common.Address(addr), common.Hash(key))
	if addrInAccessList && slotInAccessList {
		return evmc.WarmAccess
	}
	// todo(pdrobnjak): poll something similar to - https://github.com/sei-protocol/sei-v3/blob/cd50388d4d423501b15a544612643073680aa8de/execute/store/types/types.go#L22 - temporarily we can expose access via our statedb impl for testing
	return evmc.ColdAccess
}

func (h *HostContext) GetTransientStorage(addr evmc.Address, key evmc.Hash) evmc.Hash {
	return evmc.Hash(h.evm.StateDB.GetTransientState(common.Address(addr), common.Hash(key)))
}

func (h *HostContext) SetTransientStorage(addr evmc.Address, key evmc.Hash, value evmc.Hash) {
	h.evm.StateDB.SetTransientState(common.Address(addr), common.Hash(key), common.Hash(value))
}

// getEVMRevision determines the EVM revision based on the current chain configuration
func (h *HostContext) getEVMRevision() evmc.Revision {
	chainConfig := h.evm.ChainConfig()
	blockNumber := h.evm.Context.BlockNumber
	time := h.evm.Context.Time
	isMerge := h.evm.Context.Random != nil

	// Get the rules for the current block
	rules := chainConfig.Rules(blockNumber, isMerge, time)

	// Check from newest to oldest using rules
	if rules.IsPrague {
		return evmc.Prague
	}
	if rules.IsCancun {
		return evmc.Cancun
	}
	if rules.IsShanghai {
		return evmc.Shanghai
	}
	if rules.IsMerge {
		return evmc.Paris
	}
	if rules.IsLondon {
		return evmc.London
	}
	if rules.IsBerlin {
		return evmc.Berlin
	}
	if rules.IsIstanbul {
		return evmc.Istanbul
	}
	if rules.IsPetersburg {
		return evmc.Petersburg
	}
	if rules.IsConstantinople {
		return evmc.Constantinople
	}
	if rules.IsByzantium {
		return evmc.Byzantium
	}
	if rules.IsEIP158 {
		return evmc.SpuriousDragon
	}
	if rules.IsEIP150 {
		return evmc.TangerineWhistle
	}
	if rules.IsHomestead {
		return evmc.Homestead
	}
	return evmc.Frontier
}

// To be called by an exported EVM create function which knows how to instantiate params like statedb.
func createEVMWithFailFastPrecompile(blockContext vm.BlockContext, statedb vm.StateDB, chainConfig *params.ChainConfig, vmConfig vm.Config) *vm.EVM {
	return vm.NewEVM(blockContext, statedb, chainConfig, vmConfig, precompiles.AllCustomPrecompilesFailFast)
}