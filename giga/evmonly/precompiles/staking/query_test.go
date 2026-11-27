package staking

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// queryAs runs view method and returns its first result as a T.
func queryAs[T any](h *harness, method string, args ...interface{}) (T, error) {
	var out T
	values, err := h.query(method, args...)
	if err != nil {
		return out, err
	}
	abi.ConvertType(values[0], &out)
	return out, nil
}

// queryPage runs paged view method from pageKey and returns its entries and
// next key.
func queryPage[T any](h *harness, method string, arg interface{}, pageKey []byte) ([]T, []byte) {
	values, err := h.query(method, arg, pageKey)
	require.NoError(h.t, err)
	var out []T
	abi.ConvertType(values[0], &out)
	return out, values[1].([]byte)
}

// queryAll pages through paged view method from the first page and returns
// every entry.
func queryAll[T any](h *harness, method string, arg interface{}) []T {
	var all []T
	for key := []byte{}; ; {
		entries, next := queryPage[T](h, method, arg, key)
		all = append(all, entries...)
		if len(next) == 0 {
			return all
		}
		key = next
	}
}

// unpackedZero is zero as ABI unpacking builds it, so it compares equal to an
// unpacked zero.
var unpackedZero = new(big.Int).SetBytes(make([]byte, 32))

func operators(vals []Validator) []common.Address {
	out := make([]common.Address, len(vals))
	for i, v := range vals {
		out[i] = v.Operator
	}
	return out
}

func TestDelegationQuery(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))

	got, err := queryAs[Delegation](h, DelegationMethod, alice, val1)
	require.NoError(t, err)
	require.Equal(t, Delegation{Delegator: alice, Validator: val1, Shares: dec.FromInt(sei(5)), Balance: sei(5)}, got)

	_, err = queryAs[Delegation](h, DelegationMethod, bob, val1)
	require.ErrorIs(t, err, errNoDelegation)
	_, err = queryAs[Delegation](h, DelegationMethod, alice, bob)
	require.ErrorIs(t, err, errNoDelegation)
}

func TestDelegationBalanceTruncatesToWei(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	v := h.validator(val1)
	v.tokens = sei(34)
	h.store().setValidator(val1, v)

	got, err := queryAs[Delegation](h, DelegationMethod, alice, val1)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).Quo(sei(5*34), big.NewInt(35)), got.Balance, "5 of 35 shares in 34 SEI, truncated")
}

func TestValidatorQuery(t *testing.T) {
	h := newHarness(t, testParams)
	got, err := queryAs[Validator](h, ValidatorMethod, val1)
	require.NoError(t, err)
	require.Equal(t, Validator{
		Operator:                val1,
		ConsensusPubkey:         common.BytesToHash(val1[:]),
		Status:                  statusBonded,
		Tokens:                  sei(30),
		DelegatorShares:         dec.FromInt(sei(30)),
		CommissionRate:          unpackedZero,
		CommissionMaxRate:       unpackedZero,
		CommissionMaxChangeRate: unpackedZero,
		MinSelfDelegation:       sei(1),
	}, got)

	_, err = queryAs[Validator](h, ValidatorMethod, bob)
	require.ErrorIs(t, err, errNoValidatorFound)
}

func TestValidatorQueryReadsDescriptionAndCommission(t *testing.T) {
	h := newHarness(t, testParams)
	s, base := h.store(), validatorBase(val1)
	long := string(make([]byte, 40))
	require.NoError(t, s.SetString(at(base, validatorMoniker), "moniker"))
	require.NoError(t, s.SetString(at(base, validatorIdentity), "identity"))
	require.NoError(t, s.SetString(at(base, validatorWebsite), long))
	require.NoError(t, s.SetString(at(base, validatorSecurityContact), "security"))
	s.SetWord(at(base, validatorDetails), common.BigToHash(big.NewInt(2*(solstorage.MaxStringLength+1)+1)))
	s.SetUint256(at(base, validatorCommissionRate), big.NewInt(1))
	s.SetUint256(at(base, validatorCommissionMaxRate), big.NewInt(2))
	s.SetUint256(at(base, validatorCommissionMaxChange), big.NewInt(3))
	s.SetUint64(at(base, validatorCommissionUpdate), validatorCommissionUpdate.offset, 4)

	got, err := queryAs[Validator](h, ValidatorMethod, val1)
	require.NoError(t, err)
	require.Equal(t, Description{Moniker: "moniker", Identity: "identity", Website: long, SecurityContact: "security"}, got.Description,
		"a field that does not decode reads as empty")
	require.Equal(t, []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3)}, []*big.Int{got.CommissionRate, got.CommissionMaxRate, got.CommissionMaxChangeRate})
	require.Equal(t, uint64(4), got.CommissionUpdateTime)
}

func TestValidatorsQueryFiltersByStatus(t *testing.T) {
	h := newHarness(t, testParams)
	all := func(status uint8) []common.Address {
		return operators(queryAll[Validator](h, ValidatorsMethod, status))
	}
	require.Equal(t, []common.Address{val1, val2, val3}, all(0))
	require.Equal(t, []common.Address{val1, val2}, all(statusBonded))
	require.Equal(t, []common.Address{val3}, all(statusUnbonded))
	require.Empty(t, all(statusUnbonding))

	_, err := h.query(ValidatorsMethod, statusBonded+1, []byte{})
	requireReverts(t, err, errInvalidStatus)
	_, err = h.query(ValidatorsMethod, uint8(0), []byte{1})
	requireReverts(t, err, errInvalidPageKey)
	_, err = h.query(ValidatorsMethod, uint8(0), []byte{0, 0, 0, 0, 0, 0, 0, 4})
	require.ErrorIs(t, err, errInvalidPageKey, "a key past the last record")
}

func TestPagedQueriesWalkEveryEntry(t *testing.T) {
	params := testParams
	params.MaxVotingPowerEnforcementThreshold = big.NewInt(1_000_000)
	h := newGenesisHarness(t, params)
	ops := []common.Address{val1, val2, val3}
	for i := range pageLimit + 5 {
		op := common.BigToAddress(big.NewInt(int64(0x1000 + i)))
		h.fund(op, sei(1))
		require.NoError(t, h.staking.AddGenesisValidator(h.block(), h.state, GenesisValidator{
			Operator:          op,
			ConsensusPubkey:   common.BytesToHash(op[:]),
			SelfDelegation:    sei(1),
			MinSelfDelegation: sei(1),
		}))
		ops = append(ops, op)
	}
	h.endBlock()
	for _, op := range ops[3:] {
		require.NoError(t, h.delegate(alice, op, sei(1)))
		require.NoError(t, h.delegate(op, val1, sei(1)))
	}

	first, next := queryPage[Validator](h, ValidatorsMethod, uint8(0), []byte{})
	require.Len(t, first, pageLimit)
	require.NotEmpty(t, next)
	require.Equal(t, ops, operators(queryAll[Validator](h, ValidatorsMethod, uint8(0))))

	require.Equal(t, ops[3:], operators(queryAll[Validator](h, DelegatorValidatorsMethod, alice)))
	delegations := queryAll[Delegation](h, DelegatorDelegationsMethod, alice)
	require.Len(t, delegations, pageLimit+5)
	for i, d := range delegations {
		require.Equal(t, Delegation{Delegator: alice, Validator: ops[3+i], Shares: dec.FromInt(sei(1)), Balance: sei(1)}, d)
	}

	delegators := queryAll[Delegation](h, ValidatorDelegationsMethod, val1)
	require.Len(t, delegators, pageLimit+6)
	require.Equal(t, val1, delegators[0].Delegator, "the operator's self-delegation came first")
	for i, d := range delegators[1:] {
		require.Equal(t, ops[3+i], d.Delegator)
		require.Equal(t, val1, d.Validator)
	}
	h.requireInvariants(append([]common.Address{alice}, ops[3:]...)...)
}

func TestDelegatorQueries(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.delegate(alice, val3, sei(2)))

	require.Equal(t, []Delegation{
		{Delegator: alice, Validator: val1, Shares: dec.FromInt(sei(5)), Balance: sei(5)},
		{Delegator: alice, Validator: val3, Shares: dec.FromInt(sei(2)), Balance: sei(2)},
	}, queryAll[Delegation](h, DelegatorDelegationsMethod, alice))
	require.Equal(t, []common.Address{val1, val3}, operators(queryAll[Validator](h, DelegatorValidatorsMethod, alice)))

	got, err := queryAs[Validator](h, DelegatorValidatorMethod, alice, val3)
	require.NoError(t, err)
	require.Equal(t, val3, got.Operator)
	require.Equal(t, sei(12), got.Tokens)
	_, err = queryAs[Validator](h, DelegatorValidatorMethod, alice, val2)
	require.ErrorIs(t, err, errNoDelegation)

	require.Empty(t, queryAll[Delegation](h, DelegatorDelegationsMethod, bob))
	require.Empty(t, queryAll[Validator](h, DelegatorValidatorsMethod, bob))
}

func TestValidatorDelegationsQuery(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val2, sei(3)))
	require.Equal(t, []Delegation{
		{Delegator: val2, Validator: val2, Shares: dec.FromInt(sei(20)), Balance: sei(20)},
		{Delegator: alice, Validator: val2, Shares: dec.FromInt(sei(3)), Balance: sei(3)},
	}, queryAll[Delegation](h, ValidatorDelegationsMethod, val2))
	require.Empty(t, queryAll[Delegation](h, ValidatorDelegationsMethod, bob), "an unknown validator has no delegations")
}

func TestUnbondingDelegationQuery(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(2)))
	firstHeight, firstTime := h.number, h.time
	h.endBlock()
	require.NoError(t, h.undelegate(alice, val1, sei(1)))

	got, err := queryAs[UnbondingDelegation](h, UnbondingDelegationMethod, alice, val1)
	require.NoError(t, err)
	require.Equal(t, UnbondingDelegation{Delegator: alice, Validator: val1, Entries: []UnbondingDelegationEntry{
		{CreationHeight: firstHeight, CompletionTime: firstTime + testParams.UnbondingTime, InitialBalance: sei(2), Balance: sei(2)},
		{CreationHeight: h.number, CompletionTime: h.time + testParams.UnbondingTime, InitialBalance: sei(1), Balance: sei(1)},
	}}, got)

	_, err = queryAs[UnbondingDelegation](h, UnbondingDelegationMethod, bob, val1)
	require.ErrorIs(t, err, errNoUnbondingDelegation)
	h.advance(testParams.UnbondingTime + second)
	_, err = queryAs[UnbondingDelegation](h, UnbondingDelegationMethod, alice, val1)
	require.ErrorIs(t, err, errNoUnbondingDelegation, "matured entries leave")
}

func TestStagedOperationsQuery(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(10)))
	require.NoError(t, h.stageDelegate(alice, val2, sei(2)))
	require.NoError(t, h.stageDelegate(bob, val1, sei(1)))
	require.NoError(t, h.stageUndelegate(alice, val1, sei(3)))
	staged := func(delegator common.Address) []StagedOperation {
		return queryAll[StagedOperation](h, StagedOperationsMethod, delegator)
	}
	want := []StagedOperation{
		{Epoch: 1, Kind: stagedDelegate, Delegator: alice, Validator: val2, Amount: sei(2), Shares: unpackedZero},
		{Epoch: 1, Kind: stagedDelegate, Delegator: bob, Validator: val1, Amount: sei(1), Shares: unpackedZero},
		{Epoch: 1, Kind: stagedUndelegate, Delegator: alice, Validator: val1, Amount: sei(3), Shares: dec.FromInt(sei(3))},
	}
	require.Equal(t, want, staged(common.Address{}), "the zero address lists everyone's")
	require.Equal(t, []StagedOperation{want[0], want[2]}, staged(alice))
	require.Equal(t, want[1:2], staged(bob))
	require.Empty(t, staged(val1))
	got, err := queryAs[Delegation](h, DelegationMethod, alice, val1)
	require.NoError(t, err)
	require.Equal(t, dec.FromInt(sei(10)), got.Shares, "delegation counts none of them yet")

	h.endEpoch(0)
	require.Equal(t, want, staged(common.Address{}))
	h.endEpoch(1)
	require.Empty(t, h.failures)
	require.Empty(t, staged(common.Address{}), "they took effect")
	h.requireInvariants(alice, bob)
}

func TestStagedOperationAmountFollowsTheExchangeRate(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(10)))
	require.NoError(t, h.stageUndelegate(alice, val1, sei(4)))
	v := h.validator(val1)
	v.tokens = sei(20)
	h.store().setValidator(val1, v)

	got := queryAll[StagedOperation](h, StagedOperationsMethod, alice)
	require.Len(t, got, 1)
	require.Equal(t, dec.FromInt(sei(4)), got[0].Shares)
	require.Equal(t, sei(2), got[0].Amount, "4 of 40 shares in 20 SEI")
}

func TestStagedOperationsQueryPagesByQueueIndex(t *testing.T) {
	h := newHarness(t, testParams)
	for i := range pageLimit {
		require.NoError(t, h.stageDelegate(common.BigToAddress(big.NewInt(int64(0x10000+i))), val3, weiPerUsei))
	}
	h.endEpoch(0)
	for range 5 {
		require.NoError(t, h.stageDelegate(bob, val3, weiPerUsei))
	}

	first, next := queryPage[StagedOperation](h, StagedOperationsMethod, common.Address{}, []byte{})
	require.Len(t, first, pageLimit)
	require.Equal(t, binary.BigEndian.AppendUint64(nil, pageLimit), next)
	bobs, next := queryPage[StagedOperation](h, StagedOperationsMethod, bob, []byte{})
	require.Empty(t, bobs, "the first page holds none of bob's")
	require.NotEmpty(t, next)
	require.Len(t, queryAll[StagedOperation](h, StagedOperationsMethod, bob), 5)

	h.endEpoch(1)
	require.Empty(t, h.failures)
	rest, next := queryPage[StagedOperation](h, StagedOperationsMethod, common.Address{}, make([]byte, pageKeyLen))
	require.Len(t, rest, 5, "a key whose operations took effect resumes at the first still staged")
	require.Empty(t, next)
	_, err := h.query(StagedOperationsMethod, common.Address{}, binary.BigEndian.AppendUint64(nil, pageLimit+6))
	require.ErrorIs(t, err, errInvalidPageKey, "a key past the last operation")
	_, err = h.query(StagedOperationsMethod, common.Address{}, []byte{1})
	requireReverts(t, err, errInvalidPageKey)
}

func TestPoolQuery(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(2)))

	got, err := queryAs[Pool](h, PoolMethod)
	require.NoError(t, err)
	require.Equal(t, Pool{NotBondedTokens: sei(12), BondedTokens: sei(53)}, got)
	require.Equal(t, h.state.GetBalance(NotBondedEscrow), got.NotBondedTokens)
	require.Equal(t, h.state.GetBalance(BondedEscrow), got.BondedTokens)
}

func TestParamsQuery(t *testing.T) {
	h := newHarness(t, testParams)
	got, err := queryAs[StakingParams](h, ParamsMethod)
	require.NoError(t, err)
	require.Equal(t, StakingParams{
		UnbondingTime:                      testParams.UnbondingTime,
		MaxValidators:                      testParams.MaxValidators,
		MaxEntries:                         testParams.MaxEntries,
		MaxVotingPowerRatio:                testParams.MaxVotingPowerRatio,
		MaxVotingPowerEnforcementThreshold: unpackedZero,
	}, got)
}

func TestQueriesServeBeforeTheFirstBondedSet(t *testing.T) {
	h := &harness{t: t, state: newMemState(), time: startTime}
	require.NoError(t, SetParams(h.state, testParams))
	h.fund(val1, sei(1))
	require.NoError(t, h.staking.AddGenesisValidator(h.block(), h.state, GenesisValidator{
		Operator: val1, ConsensusPubkey: common.BytesToHash(val1[:]), SelfDelegation: sei(1), MinSelfDelegation: sei(1),
	}))

	got, err := queryAs[Validator](h, ValidatorMethod, val1)
	require.NoError(t, err)
	require.Equal(t, statusUnbonded, got.Status)
	pool, err := queryAs[Pool](h, PoolMethod)
	require.NoError(t, err)
	require.Equal(t, Pool{NotBondedTokens: sei(1), BondedTokens: unpackedZero}, pool)
}

func TestQueriesRejectBadCalls(t *testing.T) {
	h := newHarness(t, testParams)
	input, err := ABI.Pack(PoolMethod)
	require.NoError(t, err)
	h.fund(alice, sei(1))
	require.ErrorIs(t, h.run(alice, callOpts{value: sei(1)}, input), errNonPayable)
	require.ErrorIs(t, h.run(alice, callOpts{delegateCall: true}, input), errDelegateCall)
}
