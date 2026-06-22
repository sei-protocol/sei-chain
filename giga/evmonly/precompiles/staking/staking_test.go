package staking

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

func TestPrecompileCreateDelegateAndQuery(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	caller := common.HexToAddress("0x0000000000000000000000000000000000000abc")
	store := newMemoryStore()
	logs := &memoryLogs{}
	balances := newMemoryBalances()
	ctx := &precompiles.Context{
		Caller:        caller,
		Address:       address,
		ApparentValue: new(big.Int).Mul(big.NewInt(5), useiToSwei),
		Block:         precompiles.BlockContext{Number: 7, Time: 100},
		Store:         store,
		Balances:      balances,
		Logs:          logs,
	}

	input, err := p.abi.Pack(
		CreateValidatorMethod,
		"01020304",
		"validator-one",
		"0.100000000000000000",
		"0.200000000000000000",
		"0.010000000000000000",
		big.NewInt(1),
	)
	require.NoError(t, err)
	balances.add(address, ctx.ApparentValue)
	ret, err := p.Run(ctx, input)
	require.NoError(t, err)
	requireBoolReturn(t, p, CreateValidatorMethod, ret, true)
	require.Len(t, logs.logs, 1)
	require.Equal(t, new(big.Int).Mul(big.NewInt(5), useiToSwei), balances.balance(EscrowAddress()))
	require.Zero(t, balances.balance(caller).Sign())
	require.Zero(t, balances.balance(address).Sign())

	ctx.ApparentValue = new(big.Int).Mul(big.NewInt(2), useiToSwei)
	input, err = p.abi.Pack(DelegateMethod, caller.Hex())
	require.NoError(t, err)
	balances.add(address, ctx.ApparentValue)
	ret, err = p.Run(ctx, input)
	require.NoError(t, err)
	requireBoolReturn(t, p, DelegateMethod, ret, true)
	require.Len(t, logs.logs, 3)
	require.Equal(t, new(big.Int).Mul(big.NewInt(7), useiToSwei), balances.balance(EscrowAddress()))
	require.Zero(t, balances.balance(caller).Sign())
	require.Zero(t, balances.balance(address).Sign())

	updates, err := p.EndBlock(&precompiles.EndBlockContext{
		Address:  address,
		Block:    ctx.Block,
		Store:    store,
		Balances: balances,
		Logs:     logs,
	})
	require.NoError(t, err)
	require.Len(t, updates, 1)

	ctx.ApparentValue = nil
	input, err = p.abi.Pack(DelegationMethod, caller, caller.Hex())
	require.NoError(t, err)
	ret, err = p.Run(ctx, input)
	require.NoError(t, err)
	var delegationOut struct {
		Delegation Delegation
	}
	require.NoError(t, p.abi.UnpackIntoInterface(&delegationOut, DelegationMethod, ret))
	delegation := delegationOut.Delegation
	require.Equal(t, big.NewInt(7), delegation.Balance.Amount)
	require.Equal(t, "usei", delegation.Balance.Denom)
	require.Equal(t, caller.Hex(), delegation.Delegation.DelegatorAddress)
	require.Equal(t, caller.Hex(), delegation.Delegation.ValidatorAddress)

	input, err = p.abi.Pack(PoolMethod)
	require.NoError(t, err)
	ret, err = p.Run(ctx, input)
	require.NoError(t, err)
	var poolOut struct {
		Pool Pool
	}
	require.NoError(t, p.abi.UnpackIntoInterface(&poolOut, PoolMethod, ret))
	pool := poolOut.Pool
	require.Equal(t, "7", pool.BondedTokens)
	require.Equal(t, "0", pool.NotBondedTokens)

	input, err = p.abi.Pack(ValidatorsMethod, "BOND_STATUS_BONDED", []byte{})
	require.NoError(t, err)
	ret, err = p.Run(ctx, input)
	require.NoError(t, err)
	var validatorsOut struct {
		Response ValidatorsResponse
	}
	require.NoError(t, p.abi.UnpackIntoInterface(&validatorsOut, ValidatorsMethod, ret))
	validators := validatorsOut.Response
	require.Len(t, validators.Validators, 1)
	require.Equal(t, caller.Hex(), validators.Validators[0].OperatorAddress)
	require.Empty(t, validators.NextKey)
}

func TestPrecompileMovesNativeBalancesForStakingTransitions(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	delegator := common.HexToAddress("0x0000000000000000000000000000000000000abc")
	dstValidator := common.HexToAddress("0x0000000000000000000000000000000000000def")
	store := newMemoryStore()
	balances := newMemoryBalances()
	ctx := &precompiles.Context{
		Caller:        delegator,
		Address:       address,
		ApparentValue: new(big.Int).Mul(big.NewInt(5), useiToSwei),
		Block:         precompiles.BlockContext{Number: 7, Time: 100},
		Store:         store,
		Balances:      balances,
		Logs:          &memoryLogs{},
	}

	balances.add(address, ctx.ApparentValue)
	input, err := p.abi.Pack(
		CreateValidatorMethod,
		"01020304",
		"validator-one",
		"0.100000000000000000",
		"0.200000000000000000",
		"0.010000000000000000",
		big.NewInt(1),
	)
	require.NoError(t, err)
	_, err = p.Run(ctx, input)
	require.NoError(t, err)

	ctx.Caller = dstValidator
	ctx.ApparentValue = new(big.Int).Mul(big.NewInt(1), useiToSwei)
	balances.add(address, ctx.ApparentValue)
	input, err = p.abi.Pack(
		CreateValidatorMethod,
		"05060708",
		"validator-two",
		"0.100000000000000000",
		"0.200000000000000000",
		"0.010000000000000000",
		big.NewInt(1),
	)
	require.NoError(t, err)
	_, err = p.Run(ctx, input)
	require.NoError(t, err)

	ctx.Caller = delegator
	ctx.ApparentValue = nil
	input, err = p.abi.Pack(RedelegateMethod, delegator.Hex(), dstValidator.Hex(), big.NewInt(2))
	require.NoError(t, err)
	_, err = p.Run(ctx, input)
	require.NoError(t, err)
	src, ok, err := getValidator(store, delegator.Hex())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "3", src.Tokens)
	dst, ok, err := getValidator(store, dstValidator.Hex())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "3", dst.Tokens)
	require.Equal(t, new(big.Int).Mul(big.NewInt(6), useiToSwei), balances.balance(EscrowAddress()))
	require.Zero(t, balances.balance(delegator).Sign())
	require.Zero(t, balances.balance(dstValidator).Sign())

	input, err = p.abi.Pack(UndelegateMethod, dstValidator.Hex(), big.NewInt(1))
	require.NoError(t, err)
	_, err = p.Run(ctx, input)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).Mul(big.NewInt(6), useiToSwei), balances.balance(EscrowAddress()))
	require.Zero(t, balances.balance(delegator).Sign())
	require.Zero(t, balances.balance(dstValidator).Sign())
	require.Zero(t, balances.balance(address).Sign())

	_, err = p.EndBlock(&precompiles.EndBlockContext{
		Address: address,
		Block: precompiles.BlockContext{
			Number: 8,
			Time:   100 + 1_814_400,
		},
		Store:    store,
		Balances: balances,
		Logs:     &memoryLogs{},
	})
	require.NoError(t, err)
	require.Equal(t, new(big.Int).Mul(big.NewInt(5), useiToSwei), balances.balance(EscrowAddress()))
	require.Equal(t, new(big.Int).Mul(big.NewInt(1), useiToSwei), balances.balance(delegator))
}

func TestPrecompileRejectsDelegateCall(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	input, err := p.abi.Pack(PoolMethod)
	require.NoError(t, err)
	_, err = p.Run(&precompiles.Context{
		DelegateCall: true,
		Store:        newMemoryStore(),
	}, input)
	require.ErrorIs(t, err, errDelegateCall)
}

func requireBoolReturn(t *testing.T, p *Precompile, method string, ret []byte, expected bool) {
	t.Helper()
	values, err := p.abi.Unpack(method, ret)
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, expected, values[0])
}

type memoryStore struct {
	values map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[string][]byte{}}
}

func (s *memoryStore) Get(key []byte) ([]byte, bool) {
	value, ok := s.values[string(key)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), value...), true
}

func (s *memoryStore) Set(key []byte, value []byte) {
	s.values[string(key)] = append([]byte(nil), value...)
}

func (s *memoryStore) Delete(key []byte) {
	delete(s.values, string(key))
}

type memoryLogs struct {
	logs []*ethtypes.Log
}

func (l *memoryLogs) AddLog(log *ethtypes.Log) {
	l.logs = append(l.logs, log)
}

type memoryBalances struct {
	balances map[common.Address]*big.Int
}

func newMemoryBalances() *memoryBalances {
	return &memoryBalances{balances: map[common.Address]*big.Int{}}
}

func (b *memoryBalances) Transfer(from common.Address, to common.Address, amount *big.Int) error {
	if amount == nil || amount.Sign() == 0 {
		return nil
	}
	if amount.Sign() < 0 {
		return errors.New("negative amount")
	}
	fromBalance := b.balance(from)
	if fromBalance.Cmp(amount) < 0 {
		return errors.New("insufficient balance")
	}
	b.balances[from] = new(big.Int).Sub(fromBalance, amount)
	b.balances[to] = new(big.Int).Add(b.balance(to), amount)
	return nil
}

func (b *memoryBalances) add(addr common.Address, amount *big.Int) {
	if amount == nil || amount.Sign() == 0 {
		return
	}
	b.balances[addr] = new(big.Int).Add(b.balance(addr), amount)
}

func (b *memoryBalances) balance(addr common.Address) *big.Int {
	balance, ok := b.balances[addr]
	if !ok {
		return new(big.Int)
	}
	return new(big.Int).Set(balance)
}
