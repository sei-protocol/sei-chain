package staking

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/util"
)

func runStaking(t *testing.T, p *Precompile, store *memoryStore, balances *memoryBalances, caller common.Address, value *big.Int, method string, args ...interface{}) error {
	t.Helper()
	ctx := &precompiles.Context{
		Caller:        caller,
		Address:       address,
		ApparentValue: value,
		Block:         precompiles.BlockContext{Number: 1, Time: 100},
		Store:         store,
		Balances:      balances,
		Logs:          &memoryLogs{},
	}
	if value != nil {
		balances.add(address, value)
	}
	input, err := p.abi.Pack(method, args...)
	require.NoError(t, err)
	_, err = p.Run(ctx, input)
	return err
}

func newValidator(t *testing.T, p *Precompile, store *memoryStore, balances *memoryBalances, operator common.Address, selfStakeUsei int64) {
	t.Helper()
	err := runStaking(t, p, store, balances, operator, new(big.Int).Mul(big.NewInt(selfStakeUsei), useiToSwei),
		CreateValidatorMethod,
		"01020304",
		"moniker",
		"0.100000000000000000",
		"0.200000000000000000",
		"0.010000000000000000",
		big.NewInt(1),
	)
	require.NoError(t, err)
}

func TestRedelegateRejectsSelfRedelegation(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)
	store := newMemoryStore()
	balances := newMemoryBalances()
	valA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	newValidator(t, p, store, balances, valA, 10)

	err = runStaking(t, p, store, balances, valA, nil, RedelegateMethod, valA.Hex(), valA.Hex(), big.NewInt(1))
	require.ErrorIs(t, err, errSelfRedelegation)
}

func TestRedelegateRejectsTransitiveRedelegation(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)
	store := newMemoryStore()
	balances := newMemoryBalances()
	valA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	valB := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	valC := common.HexToAddress("0x00000000000000000000000000000000000000c3")
	newValidator(t, p, store, balances, valA, 10)
	newValidator(t, p, store, balances, valB, 10)
	newValidator(t, p, store, balances, valC, 10)

	// valA redelegates its self-delegation A -> B, so A now has a delegation to B
	// that arrived via an in-progress redelegation.
	require.NoError(t, runStaking(t, p, store, balances, valA, nil, RedelegateMethod, valA.Hex(), valB.Hex(), big.NewInt(2)))

	// Redelegating those tokens onward B -> C must be rejected as transitive.
	err = runStaking(t, p, store, balances, valA, nil, RedelegateMethod, valB.Hex(), valC.Hex(), big.NewInt(1))
	require.ErrorIs(t, err, errTransitiveRedelegation)
}

func TestRedelegateRejectsTooManyEntries(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)
	store := newMemoryStore()
	balances := newMemoryBalances()
	require.NoError(t, util.SetJSON(store, paramsKey(), Params{UnbondingTime: 1, MaxValidators: 100, MaxEntries: 1, MinCommissionRate: "0.000000000000000000"}))
	valA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	valB := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	newValidator(t, p, store, balances, valA, 10)
	newValidator(t, p, store, balances, valB, 10)

	require.NoError(t, runStaking(t, p, store, balances, valA, nil, RedelegateMethod, valA.Hex(), valB.Hex(), big.NewInt(2)))
	err = runStaking(t, p, store, balances, valA, nil, RedelegateMethod, valA.Hex(), valB.Hex(), big.NewInt(2))
	require.ErrorIs(t, err, errMaxRedelegationEntries)
}

func TestUndelegateRejectsTooManyEntries(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)
	store := newMemoryStore()
	balances := newMemoryBalances()
	require.NoError(t, util.SetJSON(store, paramsKey(), Params{UnbondingTime: 1, MaxValidators: 100, MaxEntries: 1, MinCommissionRate: "0.000000000000000000"}))
	valA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	newValidator(t, p, store, balances, valA, 10)

	require.NoError(t, runStaking(t, p, store, balances, valA, nil, UndelegateMethod, valA.Hex(), big.NewInt(2)))
	err = runStaking(t, p, store, balances, valA, nil, UndelegateMethod, valA.Hex(), big.NewInt(2))
	require.ErrorIs(t, err, errMaxUnbondingEntries)
}

func TestCreateValidatorRejectsBadCommission(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)
	store := newMemoryStore()
	balances := newMemoryBalances()
	valA := common.HexToAddress("0x00000000000000000000000000000000000000a1")

	// rate greater than the max rate
	err = runStaking(t, p, store, balances, valA, new(big.Int).Mul(big.NewInt(10), useiToSwei),
		CreateValidatorMethod, "01020304", "moniker",
		"0.300000000000000000", "0.200000000000000000", "0.010000000000000000", big.NewInt(1))
	require.ErrorIs(t, err, errCommissionGTMaxRate)

	// max rate greater than 100%
	err = runStaking(t, p, store, balances, valA, new(big.Int).Mul(big.NewInt(10), useiToSwei),
		CreateValidatorMethod, "01020304", "moniker",
		"0.100000000000000000", "1.500000000000000000", "0.010000000000000000", big.NewInt(1))
	require.ErrorIs(t, err, errCommissionHuge)
}

func TestValidateInitialCommission(t *testing.T) {
	require.NoError(t, validateInitialCommission("0.1", "0.2", "0.01", "0"))
	require.NoError(t, validateInitialCommission("0.05", "0.05", "0.05", "0.05"))
	require.ErrorIs(t, validateInitialCommission("-0.1", "0.2", "0.01", "0"), errCommissionNegative)
	require.ErrorIs(t, validateInitialCommission("0.3", "0.2", "0.01", "0"), errCommissionGTMaxRate)
	require.ErrorIs(t, validateInitialCommission("0.1", "1.1", "0.01", "0"), errCommissionHuge)
	require.ErrorIs(t, validateInitialCommission("0.1", "0.2", "0.3", "0"), errCommissionChangeGTMaxRate)
	require.ErrorIs(t, validateInitialCommission("0.01", "0.2", "0.01", "0.05"), errCommissionLTMinRate)
	// fraction and scientific notation forms are rejected.
	require.Error(t, validateInitialCommission("1/3", "0.2", "0.01", "0"))
	require.Error(t, validateInitialCommission("1e-1", "0.2", "0.01", "0"))
}

func TestValidateCommissionUpdate(t *testing.T) {
	validator := Validator{
		CommissionRate:          "0.100000000000000000",
		CommissionMaxRate:       "0.200000000000000000",
		CommissionMaxChangeRate: "0.010000000000000000",
		CommissionUpdateTime:    0,
	}
	dayLater := uint64(commissionUpdateMinInterval)

	require.NoError(t, validateCommissionUpdate(validator, "0.105", "0", dayLater))
	// too soon after the last change
	require.ErrorIs(t, validateCommissionUpdate(validator, "0.105", "0", dayLater-1), errCommissionUpdateTime)
	// change larger than the max change rate
	require.ErrorIs(t, validateCommissionUpdate(validator, "0.2", "0", dayLater), errCommissionGTMaxChange)
	// below the min commission rate
	require.ErrorIs(t, validateCommissionUpdate(validator, "0.05", "0.08", dayLater), errCommissionLTMinRate)
}
