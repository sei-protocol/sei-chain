package evmc

import (
	"github.com/ethereum/evmc/v12/bindings/go/evmc"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

var _ evmc.HostContext = (*HostContext)(nil)

type HostContext struct {
	vm      *evmc.VM
	stateDB vm.StateDB
}

func NewHostContext(vm *evmc.VM, stateDB vm.StateDB) *HostContext {
	return &HostContext{vm: vm, stateDB: stateDB}
}

func (h *HostContext) AccountExists(addr evmc.Address) bool {
	return h.stateDB.Exist(common.Address(addr))
}

func (h *HostContext) GetStorage(addr evmc.Address, key evmc.Hash) evmc.Hash {
	return evmc.Hash(h.stateDB.GetState(common.Address(addr), common.Hash(key)))
}

// todo(pdrobnjak): implement this
func (h *HostContext) SetStorage(addr evmc.Address, key evmc.Hash, value evmc.Hash) evmc.StorageStatus {
	_ = h.stateDB.SetState(common.Address(addr), common.Hash(key), common.Hash(value))
	return evmc.StorageAdded
}

func (h *HostContext) GetBalance(addr evmc.Address) evmc.Hash {
	return h.stateDB.GetBalance(common.Address(addr)).Bytes32()
}

func (h *HostContext) GetCodeSize(addr evmc.Address) int {
	return h.stateDB.GetCodeSize(common.Address(addr))
}

func (h *HostContext) GetCodeHash(addr evmc.Address) evmc.Hash {
	return evmc.Hash(h.stateDB.GetCodeHash(common.Address(addr)))
}

func (h *HostContext) GetCode(addr evmc.Address) []byte {
	return h.stateDB.GetCode(common.Address(addr))
}

// todo(pdrobnjak): implement this
func (h *HostContext) Selfdestruct(_ evmc.Address, _ evmc.Address) bool {
	return false
}

// todo(pdrobnjak): implement this
func (h *HostContext) GetTxContext() evmc.TxContext {
	return evmc.TxContext{}
}

// todo(pdrobnjak): implement this
func (h *HostContext) GetBlockHash(_ int64) evmc.Hash {
	return evmc.Hash{}
}

// todo(pdrobnjak): convert topics
func (h *HostContext) EmitLog(addr evmc.Address, _ []evmc.Hash, data []byte) {
	h.stateDB.AddLog(&ethtypes.Log{Address: common.Address(addr), Topics: nil, Data: data})
}

// todo(pdrobnjak): figure out how to populate - evmRevision, delegated, code - probably can be passed down from interpreter
func (h *HostContext) Call(
	kind evmc.CallKind, recipient evmc.Address, sender evmc.Address, value evmc.Hash, input []byte, gas int64,
	depth int, static bool, _ evmc.Hash, _ evmc.Address,
) (output []byte, gasLeft int64, gasRefund int64, createAddr evmc.Address, err error) {
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

// todo(pdrobnjak): implement this
func (h *HostContext) AccessAccount(_ evmc.Address) evmc.AccessStatus {
	return evmc.ColdAccess
}

// todo(pdrobnjak): implement this
func (h *HostContext) AccessStorage(_ evmc.Address, _ evmc.Hash) evmc.AccessStatus {
	return evmc.ColdAccess
}

func (h *HostContext) GetTransientStorage(addr evmc.Address, key evmc.Hash) evmc.Hash {
	return evmc.Hash(h.stateDB.GetTransientState(common.Address(addr), common.Hash(key)))
}

func (h *HostContext) SetTransientStorage(addr evmc.Address, key evmc.Hash, value evmc.Hash) {
	h.stateDB.SetTransientState(common.Address(addr), common.Hash(key), common.Hash(value))
}
