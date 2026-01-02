package evmc

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
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
	h.evm.TxContext.GasPrice.FillBytes(gasPrice[:])

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

	return evmc.TxContext{
		GasPrice:    gasPrice,
		Origin:      evmc.Address(h.evm.TxContext.Origin),
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
	return evmc.Hash(h.evm.Context.GetHash(uint64(number)))
}

func (h *HostContext) EmitLog(addr evmc.Address, topics []evmc.Hash, data []byte) {
	gethTopics := make([]common.Hash, len(topics))
	for i, topic := range topics {
		gethTopics[i] = common.Hash(topic)
	}
	h.evm.StateDB.AddLog(&ethtypes.Log{Address: common.Address(addr), Topics: gethTopics, Data: data})
}

// todo(pdrobnjak): figure out how to populate - evmRevision, delegated, code - probably can be passed down from interpreter
// this will sometimes be called throught interpreter.Run (top level) and sometimes from evmc_execute (child calls)
// which means that sometimes it should delegate to the interpreter and sometimes it should call evm.Call/evm.DelegateCall/...
// we are getting a Frankestein of geth + evmc + evmone
// can this be routed through depth? noup, but we can set an internal flag in HostContext when calling through interpreter.Run
// host HostContext needs to contain the EVM
func (h *HostContext) Call(
	kind evmc.CallKind, recipient evmc.Address, sender evmc.Address, value evmc.Hash, input []byte, gas int64,
	depth int, static bool, salt evmc.Hash, codeAddress evmc.Address,
) ([]byte, int64, int64, evmc.Address, error) {
	// evmc -> opdelegatecall -> HostContext.Call (here we should route ) -> evm.DelegateCall -> intepreter.Run -> HostContext.Call
	flag := true
	if flag {
		switch kind {
		case evmc.Call:
			ret, leftoverGas, err := h.evm.Call(caller, addr, input, gas, value)
		case evmc.DelegateCall:
			ret, leftoverGas, err := h.evm.DelegateCall(originCaller, caller, addr, input, gas, value)
		case evmc.CallCode:
			ret, leftoverGas, err := h.evm.CallCode(caller, addr, input, gas, value)
		case evmc.Create:
			ret, createAddr, leftoverGas, err := h.evm.Create(caller, code, gas, value)
		case evmc.Create2:
			ret, createAddr, leftoverGas, err := h.evm.Create2(caller, code, gas, endowment, salt)
		}
	}
	// ELSE
	evmRevision := evmc.Frontier
	delegated := false
	var code []byte
	executionResult, err := h.vm.Execute(
		h, evmRevision, kind, static, delegated, depth,
		gas, recipient, sender, input, value, code,
	)
	if err != nil {
		return nil, 0, 0, [20]byte{}, err
	}

	//todo(pdrobnjak): figure out how to populate createAddr
	return executionResult.Output, executionResult.GasLeft, executionResult.GasRefund, evmc.Address{}, nil
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
