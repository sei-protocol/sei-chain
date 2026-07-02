package evmonly

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var errInvalidPrecompileStateDB = errors.New("evm-only precompile requires native state db")

type unresolvedCustomPrecompile struct{}

func (unresolvedCustomPrecompile) RequiredGas([]byte) uint64 {
	return 0
}

func (unresolvedCustomPrecompile) Run(*vm.EVM, common.Address, common.Address, []byte, *big.Int, bool, bool, *tracing.Hooks) ([]byte, error) {
	return nil, precompiles.ErrCustomPrecompilesOpen
}

type registeredCustomPrecompile struct {
	address  common.Address
	contract precompiles.Contract
}

func (p registeredCustomPrecompile) RequiredGas(input []byte) uint64 {
	return p.contract.RequiredGas(input)
}

func (p registeredCustomPrecompile) Run(evm *vm.EVM, caller common.Address, _ common.Address, input []byte, value *big.Int, readOnly bool, isFromDelegateCall bool, _ *tracing.Hooks) ([]byte, error) {
	return p.run(evm, caller, input, value, readOnly, isFromDelegateCall, 0)
}

func (p registeredCustomPrecompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, _ common.Address, input []byte, suppliedGas uint64, value *big.Int, hooks *tracing.Hooks, readOnly bool, isFromDelegateCall bool) ([]byte, uint64, error) {
	gasCost := p.RequiredGas(input)
	if suppliedGas < gasCost {
		return nil, 0, vm.ErrOutOfGas
	}
	remainingGas := suppliedGas - gasCost
	if hooks != nil && hooks.OnGasChange != nil {
		hooks.OnGasChange(suppliedGas, remainingGas, tracing.GasChangeCallPrecompiledContract)
	}
	ret, err := p.run(evm, caller, input, value, readOnly, isFromDelegateCall, remainingGas)
	return ret, remainingGas, err
}

func (p registeredCustomPrecompile) run(evm *vm.EVM, caller common.Address, input []byte, value *big.Int, readOnly bool, isFromDelegateCall bool, remainingGas uint64) ([]byte, error) {
	stateDB, ok := evm.StateDB.(*nativeStateDB)
	if !ok {
		return nil, errInvalidPrecompileStateDB
	}
	ctx := &precompiles.Context{
		Caller:        caller,
		Address:       p.address,
		ApparentValue: cloneBig(value),
		ReadOnly:      readOnly,
		DelegateCall:  isFromDelegateCall,
		GasRemaining:  remainingGas,
		Block:         evmPrecompileBlockContext(evm),
		Store:         storageBackedStore{db: stateDB, address: p.address},
		Balances:      nativeBalanceTransfer{db: stateDB},
		Logs:          stateDB,
	}
	return p.contract.Run(ctx, input)
}

func customPrecompileMap(registry precompiles.Registry) map[common.Address]vm.PrecompiledContract {
	if registry == nil {
		return nil
	}
	addresses := registry.Addresses()
	if len(addresses) == 0 {
		return nil
	}
	contracts := make(map[common.Address]vm.PrecompiledContract, len(addresses))
	for _, addr := range addresses {
		contract, ok := registry.Get(addr)
		if !ok || contract == nil {
			contracts[addr] = unresolvedCustomPrecompile{}
			continue
		}
		contracts[addr] = registeredCustomPrecompile{
			address:  addr,
			contract: contract,
		}
	}
	return contracts
}

func runCustomPrecompileEndBlock(registry precompiles.Registry, evm *vm.EVM) ([]precompiles.ValidatorUpdate, error) {
	if registry == nil {
		return nil, nil
	}
	stateDB, ok := evm.StateDB.(*nativeStateDB)
	if !ok {
		return nil, errInvalidPrecompileStateDB
	}
	addresses := registry.Addresses()
	updates := make([]precompiles.ValidatorUpdate, 0)
	for _, addr := range addresses {
		contract, ok := registry.Get(addr)
		if !ok || contract == nil {
			continue
		}
		endBlocker, ok := contract.(precompiles.EndBlocker)
		if !ok {
			continue
		}
		ctx := &precompiles.EndBlockContext{
			Address:  addr,
			Block:    evmPrecompileBlockContext(evm),
			Store:    storageBackedStore{db: stateDB, address: addr},
			Balances: nativeBalanceTransfer{db: stateDB},
			Logs:     stateDB,
		}
		contractUpdates, err := endBlocker.EndBlock(ctx)
		if err != nil {
			return nil, err
		}
		updates = append(updates, contractUpdates...)
	}
	return updates, nil
}

func evmPrecompileBlockContext(evm *vm.EVM) precompiles.BlockContext {
	var number uint64
	if evm.Context.BlockNumber != nil {
		number = evm.Context.BlockNumber.Uint64()
	}
	var chainID *big.Int
	if cfg := evm.ChainConfig(); cfg != nil && cfg.ChainID != nil {
		chainID = new(big.Int).Set(cfg.ChainID)
	}
	var prevRandao common.Hash
	if evm.Context.Random != nil {
		prevRandao = *evm.Context.Random
	}
	return precompiles.BlockContext{
		Number:      number,
		Time:        evm.Context.Time,
		ChainID:     chainID,
		BaseFee:     cloneBig(evm.Context.BaseFee),
		BlobBaseFee: cloneBig(evm.Context.BlobBaseFee),
		Coinbase:    evm.Context.Coinbase,
		PrevRandao:  prevRandao,
	}
}

type nativeBalanceTransfer struct {
	db *nativeStateDB
}

func (t nativeBalanceTransfer) Transfer(from common.Address, to common.Address, amount *big.Int) error {
	if amount == nil || amount.Sign() == 0 {
		return nil
	}
	if t.db.err != nil {
		return t.db.err
	}
	u, err := uint256FromBigChecked(amount)
	if err != nil {
		t.db.err = err
		return err
	}
	if t.db.GetBalance(from).Cmp(u) < 0 {
		t.db.err = errInsufficientBalance
		return errInsufficientBalance
	}
	t.db.SubBalance(from, u, tracing.BalanceChangeTransfer)
	if t.db.err != nil {
		return t.db.err
	}
	t.db.AddBalance(to, u, tracing.BalanceChangeTransfer)
	return t.db.err
}

func uint256FromBigChecked(v *big.Int) (*uint256.Int, error) {
	if v == nil {
		return uint256.NewInt(0), nil
	}
	if v.Sign() < 0 {
		return nil, errors.New("negative amount")
	}
	u, overflow := uint256.FromBig(v)
	if overflow {
		return nil, errors.New("amount exceeds uint256")
	}
	if u == nil {
		return uint256.NewInt(0), nil
	}
	return u, nil
}

const (
	storeLengthDomain = "sei/evmonly/precompile-store/length/v1"
	storeChunkDomain  = "sei/evmonly/precompile-store/chunk/v1"
)

type storageBackedStore struct {
	db      *nativeStateDB
	address common.Address
}

func (s storageBackedStore) Get(key []byte) ([]byte, bool) {
	baseSlot := storeBaseSlot(key)
	length, ok := s.length(baseSlot)
	if !ok {
		return nil, false
	}
	if length > uint64(^uint(0)>>1) {
		return nil, false
	}
	chunks := chunkCount(length)
	out := make([]byte, 0, int(chunks*32)) //nolint:gosec // length was bounded by max int above.
	for i := uint64(0); i < chunks; i++ {
		chunk := s.db.GetState(s.address, storeChunkSlot(baseSlot, i))
		out = append(out, chunk.Bytes()...)
	}
	return out[:int(length)], true //nolint:gosec // length was bounded by max int above.
}

func (s storageBackedStore) Set(key []byte, value []byte) {
	baseSlot := storeBaseSlot(key)
	oldLength, oldOK := s.length(baseSlot)
	oldChunks := uint64(0)
	if oldOK {
		oldChunks = chunkCount(oldLength)
	}
	newLength := uint64(len(value)) //nolint:gosec // slices cannot exceed max int.
	newChunks := chunkCount(newLength)
	s.db.SetState(s.address, baseSlot, encodedStoredLength(newLength))
	for i := uint64(0); i < newChunks; i++ {
		start := int(i * 32) //nolint:gosec // i is bounded by len(value) chunks.
		end := start + 32
		if end > len(value) {
			end = len(value)
		}
		var chunk common.Hash
		copy(chunk[:], value[start:end])
		s.db.SetState(s.address, storeChunkSlot(baseSlot, i), chunk)
	}
	for i := newChunks; i < oldChunks; i++ {
		s.db.SetState(s.address, storeChunkSlot(baseSlot, i), common.Hash{})
	}
}

func (s storageBackedStore) Delete(key []byte) {
	baseSlot := storeBaseSlot(key)
	length, ok := s.length(baseSlot)
	if !ok {
		return
	}
	for i := uint64(0); i < chunkCount(length); i++ {
		s.db.SetState(s.address, storeChunkSlot(baseSlot, i), common.Hash{})
	}
	s.db.SetState(s.address, baseSlot, common.Hash{})
}

func (s storageBackedStore) length(baseSlot common.Hash) (uint64, bool) {
	encoded := s.db.GetState(s.address, baseSlot)
	if encoded == (common.Hash{}) {
		return 0, false
	}
	n := encoded.Big()
	if n.Sign() == 0 {
		return 0, false
	}
	n.Sub(n, big.NewInt(1))
	if !n.IsUint64() {
		return 0, false
	}
	return n.Uint64(), true
}

func storeBaseSlot(key []byte) common.Hash {
	return crypto.Keccak256Hash([]byte(storeLengthDomain), key)
}

func storeChunkSlot(baseSlot common.Hash, index uint64) common.Hash {
	var indexBz [8]byte
	binary.BigEndian.PutUint64(indexBz[:], index)
	return crypto.Keccak256Hash([]byte(storeChunkDomain), baseSlot.Bytes(), indexBz[:])
}

func encodedStoredLength(length uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(length + 1))
}

func chunkCount(length uint64) uint64 {
	if length == 0 {
		return 0
	}
	return (length + 31) / 32
}
