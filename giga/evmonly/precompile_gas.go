package evmonly

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

const maxGas = ^uint64(0)

type precompileGasMeter struct {
	remaining uint64
	hooks     *tracing.Hooks
	err       error
}

func newPrecompileGasMeter(suppliedGas uint64, hooks *tracing.Hooks) *precompileGasMeter {
	return &precompileGasMeter{
		remaining: suppliedGas,
		hooks:     hooks,
	}
}

func (m *precompileGasMeter) remainingGas() uint64 {
	if m == nil {
		return 0
	}
	return m.remaining
}

func (m *precompileGasMeter) charge(gas uint64, reason tracing.GasChangeReason) bool {
	if m == nil {
		return true
	}
	if m.err != nil {
		return false
	}
	if gas == 0 {
		return true
	}
	if m.remaining < gas {
		m.fail(vm.ErrOutOfGas, reason)
		return false
	}
	old := m.remaining
	m.remaining -= gas
	m.emitGasChange(old, m.remaining, reason)
	return true
}

func (m *precompileGasMeter) fail(err error, reason tracing.GasChangeReason) {
	if m.err == nil {
		m.err = err
	}
	if m.remaining == 0 {
		return
	}
	old := m.remaining
	m.remaining = 0
	m.emitGasChange(old, 0, reason)
}

func (m *precompileGasMeter) emitGasChange(old uint64, next uint64, reason tracing.GasChangeReason) {
	if m.hooks != nil && m.hooks.OnGasChange != nil {
		m.hooks.OnGasChange(old, next, reason)
	}
}

func (m *precompileGasMeter) chargeKeccak(size int) bool {
	sizeU64 := uint64(size) //nolint:gosec // slice/string lengths are non-negative and bounded by max int.
	words := wordCount(sizeU64)
	return m.charge(gasAdd(params.Keccak256Gas, gasMul(params.Keccak256WordGas, words)), tracing.GasChangeCallPrecompiledContract)
}

func (m *precompileGasMeter) chargeSLoad(db *nativeStateDB, addr common.Address, slot common.Hash) bool {
	if _, slotPresent := db.SlotInAccessList(addr, slot); !slotPresent {
		db.AddSlotToAccessList(addr, slot)
		return m.charge(params.ColdSloadCostEIP2929, tracing.GasChangeCallStorageColdAccess)
	}
	return m.charge(params.WarmStorageReadCostEIP2929, tracing.GasChangeCallPrecompiledContract)
}

func (m *precompileGasMeter) chargeSStore(db *nativeStateDB, addr common.Address, slot common.Hash, value common.Hash) bool {
	if m.remaining <= params.SstoreSentryGasEIP2200 {
		m.fail(vm.ErrOutOfGas, tracing.GasChangeCallPrecompiledContract)
		return false
	}
	cost := uint64(0)
	if _, slotPresent := db.SlotInAccessList(addr, slot); !slotPresent {
		cost = params.ColdSloadCostEIP2929
		db.AddSlotToAccessList(addr, slot)
	}
	current := db.GetState(addr, slot)
	if current == value {
		return m.charge(gasAdd(cost, params.WarmStorageReadCostEIP2929), tracing.GasChangeCallPrecompiledContract)
	}
	original := db.GetCommittedState(addr, slot)
	if original == current {
		if original == (common.Hash{}) {
			return m.charge(gasAdd(cost, params.SstoreSetGasEIP2200), tracing.GasChangeCallPrecompiledContract)
		}
		if value == (common.Hash{}) {
			db.AddRefund(params.SstoreClearsScheduleRefundEIP3529)
		}
		return m.charge(gasAdd(cost, params.SstoreResetGasEIP2200-params.ColdSloadCostEIP2929), tracing.GasChangeCallPrecompiledContract)
	}
	m.adjustDirtySStoreRefund(db, current, original, value)
	return m.charge(gasAdd(cost, params.WarmStorageReadCostEIP2929), tracing.GasChangeCallPrecompiledContract)
}

func (m *precompileGasMeter) adjustDirtySStoreRefund(db *nativeStateDB, current common.Hash, original common.Hash, value common.Hash) {
	if original != (common.Hash{}) {
		if current == (common.Hash{}) {
			db.SubRefund(params.SstoreClearsScheduleRefundEIP3529)
		} else if value == (common.Hash{}) {
			db.AddRefund(params.SstoreClearsScheduleRefundEIP3529)
		}
	}
	if original != value {
		return
	}
	if original == (common.Hash{}) {
		db.AddRefund(params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929)
		return
	}
	db.AddRefund((params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929)
}

func (m *precompileGasMeter) chargeNativeTransfer(db *nativeStateDB, from common.Address, to common.Address, amount *big.Int) bool {
	if amount == nil || amount.Sign() == 0 {
		return true
	}
	if !m.chargeAccountAccess(db, from) || !m.chargeAccountAccess(db, to) {
		return false
	}
	if !m.charge(params.CallValueTransferGas, tracing.GasChangeCallPrecompiledContract) {
		return false
	}
	if db.Empty(to) {
		return m.charge(params.CallNewAccountGas, tracing.GasChangeCallPrecompiledContract)
	}
	return true
}

func (m *precompileGasMeter) chargeAccountAccess(db *nativeStateDB, addr common.Address) bool {
	if db.AddressInAccessList(addr) {
		return m.charge(params.WarmStorageReadCostEIP2929, tracing.GasChangeCallPrecompiledContract)
	}
	db.AddAddressToAccessList(addr)
	return m.charge(params.ColdAccountAccessCostEIP2929, tracing.GasChangeCallStorageColdAccess)
}

func (m *precompileGasMeter) chargeLog(topics int, dataLen int) bool {
	topicsGas := gasMul(params.LogTopicGas, uint64(topics)) //nolint:gosec // topic count is bounded by log construction.
	dataGas := gasMul(params.LogDataGas, uint64(dataLen))   //nolint:gosec // log data length is bounded by memory.
	return m.charge(gasAdd(params.LogGas, topicsGas, dataGas), tracing.GasChangeCallPrecompiledContract)
}

type meteredLogSink struct {
	sink  precompiles.LogSink
	meter *precompileGasMeter
}

func (l meteredLogSink) AddLog(log *ethtypes.Log) {
	if l.sink == nil || log == nil {
		return
	}
	if l.meter != nil && !l.meter.chargeLog(len(log.Topics), len(log.Data)) {
		return
	}
	l.sink.AddLog(log)
}

func (s storageBackedStore) chargeStoreBaseSlot(key []byte) bool {
	if s.meter == nil {
		return true
	}
	return s.meter.chargeKeccak(len(storeLengthDomain) + len(key))
}

func (s storageBackedStore) chargeStoreChunkSlot(baseSlot common.Hash, index uint64) bool {
	if s.meter == nil {
		return true
	}
	return s.meter.chargeKeccak(len(storeChunkDomain) + len(baseSlot) + 8)
}

func (s storageBackedStore) chargeSLoad(slot common.Hash) bool {
	if s.meter == nil {
		return true
	}
	return s.meter.chargeSLoad(s.db, s.address, slot)
}

func (s storageBackedStore) chargeSStore(slot common.Hash, value common.Hash) bool {
	if s.meter == nil {
		return true
	}
	return s.meter.chargeSStore(s.db, s.address, slot, value)
}

func wordCount(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	return (size + 31) / 32
}

func gasAdd(values ...uint64) uint64 {
	total := uint64(0)
	for _, value := range values {
		if maxGas-total < value {
			return maxGas
		}
		total += value
	}
	return total
}

func gasMul(left uint64, right uint64) uint64 {
	if left != 0 && right > maxGas/left {
		return maxGas
	}
	return left * right
}
