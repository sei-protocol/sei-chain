package staking

import (
	"bytes"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

func TestDelegateIssuesSharesAndBondsTheTokens(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))

	require.Equal(t, dec.FromInt(sei(5)), h.shares(alice, val1), "a validator's first exchange rate is one share per token")
	v := h.validator(val1)
	require.Equal(t, sei(35), v.tokens)
	require.Equal(t, dec.FromInt(sei(35)), v.delegatorShares)
	require.Equal(t, sei(55), h.state.GetBalance(BondedEscrow))
	require.Zero(t, h.state.GetBalance(Address).Sign(), "the call's value leaves the precompile")
	require.True(t, h.store().delegatedValidators(alice).Contains(common.BytesToHash(val1[:])))

	log := h.state.logs[len(h.state.logs)-1]
	require.Equal(t, []common.Hash{ABI.Events[DelegateEvent].ID, addressTopic(alice), addressTopic(val1)}, log.Topics)
	data, err := ABI.Events[DelegateEvent].Inputs.NonIndexed().Unpack(log.Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{sei(5), dec.FromInt(sei(5))}, data)
	h.requireInvariants(alice)
}

func TestDelegateToAnUnbondedValidatorFundsTheNotBondedEscrow(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val3, sei(5)))
	require.Equal(t, sei(15), h.state.GetBalance(NotBondedEscrow))
	h.requireInvariants(alice)
}

func TestDelegateRejectsBadCalls(t *testing.T) {
	h := newHarness(t, testParams)
	requireReverts(t, h.call(alice, callOpts{}, DelegateMethod, val1), errInvalidAmount)
	h.fund(alice, sei(1))
	requireReverts(t, h.call(alice, callOpts{value: big.NewInt(1)}, DelegateMethod, val1), errInvalidAmount)
	require.ErrorIs(t, h.delegate(alice, bob, sei(1)), errNoValidatorFound)
	require.ErrorIs(t, h.call(alice, callOpts{value: sei(1), readOnly: true}, DelegateMethod, val1), errStaticCall)
	require.ErrorIs(t, h.call(alice, callOpts{value: sei(1), delegateCall: true}, DelegateMethod, val1), errDelegateCall)
	h.fund(alice, sei(1))
	require.ErrorIs(t, h.call(alice, callOpts{value: sei(1)}, UndelegateMethod, val1, sei(1)), errNonPayable)
	require.ErrorIs(t, h.run(alice, callOpts{}, []byte{1, 2, 3}), errUnknownMethod)
	h.requireInvariants(alice)
}

func TestCallsWaitForTheFirstBondedSet(t *testing.T) {
	h := &harness{t: t, state: newMemState(), time: startTime}
	require.ErrorIs(t, h.delegate(alice, val1, sei(1)), errNotStarted)
	failures, err := Precompile{}.EndBlock(h.block(), h.state)
	require.NoError(t, err)
	require.Empty(t, failures)
	require.Empty(t, h.state.storage, "EndBlock writes nothing before staking's params")

	require.NoError(t, SetParams(h.state, testParams))
	h.fund(val1, sei(10))
	require.NoError(t, h.staking.AddGenesisValidator(h.block(), h.state, GenesisValidator{
		Operator:          val1,
		ConsensusPubkey:   common.BytesToHash(val1[:]),
		SelfDelegation:    sei(10),
		MinSelfDelegation: sei(1),
	}))
	require.ErrorIs(t, h.delegate(alice, val1, sei(1)), errNotStarted, "nothing is bonded before the first EndBlock")
	h.endBlock()
	require.NoError(t, h.delegate(alice, val1, sei(1)))
}

func TestUndelegateQueuesAnUnbondingEntry(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(2)))

	require.Equal(t, dec.FromInt(sei(3)), h.shares(alice, val1))
	require.Equal(t, sei(33), h.validator(val1).tokens)
	completion := h.time + testParams.UnbondingTime
	require.Equal(t, []unbondingEntry{{creationHeight: h.number, completionTime: completion, initialBalance: sei(2), balance: sei(2)}},
		must(h.store().unbondingEntries(alice, val1)))
	q := h.store().unbondingQueue()
	require.Equal(t, uint64(1), q.tail()-q.head())
	require.Equal(t, sei(53), h.state.GetBalance(BondedEscrow))
	require.Equal(t, sei(12), h.state.GetBalance(NotBondedEscrow))

	log := h.state.logs[len(h.state.logs)-1]
	require.Equal(t, []common.Hash{ABI.Events[UndelegateEvent].ID, addressTopic(alice), addressTopic(val1)}, log.Topics)
	data, err := ABI.Events[UndelegateEvent].Inputs.NonIndexed().Unpack(log.Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{sei(2), completion}, data)
	h.requireInvariants(alice)
}

func TestUndelegatingEverythingRemovesTheDelegation(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(5)))
	_, found := h.store().delegation(alice, val1)
	require.False(t, found)
	require.Zero(t, h.store().delegatedValidators(alice).Len())
	h.requireInvariants(alice)
}

func TestUndelegateRejectsBadCalls(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	requireReverts(t, h.undelegate(alice, val1, new(big.Int)), errInvalidAmount)
	requireReverts(t, h.undelegate(alice, val1, big.NewInt(1)), errInvalidAmount)
	require.ErrorIs(t, h.undelegate(alice, val1, sei(6)), errInvalidShares)
	require.ErrorIs(t, h.undelegate(alice, bob, sei(1)), errNoValidatorFound)
	require.ErrorIs(t, h.undelegate(bob, val1, sei(1)), errNoDelegation)
	h.requireInvariants(alice)
}

func TestUndelegateStopsAtMaxEntries(t *testing.T) {
	params := testParams
	params.MaxEntries = 2
	h := newHarness(t, params)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.undelegate(alice, val1, sei(1)))
	require.NoError(t, h.undelegate(alice, val1, sei(1)))
	require.ErrorIs(t, h.undelegate(alice, val1, sei(1)), errMaxUnbondingEntries)
	require.NoError(t, h.delegate(alice, val2, sei(1)))
	require.NoError(t, h.undelegate(alice, val2, sei(1)), "each validator has its own entries")
	h.advance(params.UnbondingTime)
	require.NoError(t, h.undelegate(alice, val1, sei(1)), "completed entries free their place")
	h.requireInvariants(alice)
}

func TestSelfUndelegationBelowTheMinimumDoesNotJail(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val2, sei(15)))
	require.NoError(t, h.undelegate(val2, val2, sei(20)))
	v := h.validator(val2)
	require.False(t, v.jailed)
	require.Equal(t, sei(15).String(), v.tokens.String())
	require.Equal(t, []common.Address{val1, val2, val3}, h.powerIndex(), "val2 keeps its place on alice's stake")
	h.requireInvariants(alice)

	h.endBlock()
	require.Empty(t, h.failures)
	require.Equal(t, []validatorPower{{val1, 30}, {val2, 15}}, h.lastValidators(), "val2 stays bonded with no self-delegation")
	h.requireInvariants(alice)
}

func TestRedelegateMovesTheStakeAndQueuesTheEntry(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.redelegate(alice, val1, val2, sei(2)))

	require.Equal(t, dec.FromInt(sei(3)), h.shares(alice, val1))
	require.Equal(t, dec.FromInt(sei(2)), h.shares(alice, val2))
	require.Equal(t, sei(33), h.validator(val1).tokens)
	require.Equal(t, sei(22), h.validator(val2).tokens)
	require.Equal(t, big.NewInt(1), h.store().redelegationEntryCount(alice, val1, val2))
	require.Equal(t, big.NewInt(1), h.store().receivingRedelegations(val2, alice))

	s, q := h.store(), h.store().redelegationQueue()
	require.Equal(t, uint64(1), q.tail()-q.head())
	e := q.entry(q.head())
	completion := h.time + testParams.UnbondingTime
	require.Equal(t, completion, s.Uint64(at(e, queueCompletionTime), queueCompletionTime.offset))
	require.Equal(t, alice, s.AddressAt(at(e, redelegationQueueDelegator), redelegationQueueDelegator.offset))
	require.Equal(t, h.number, s.Uint64(at(e, redelegationQueueCreationHeight), redelegationQueueCreationHeight.offset))
	require.Equal(t, val1, s.AddressAt(at(e, redelegationQueueSrcValidator), redelegationQueueSrcValidator.offset))
	require.Equal(t, val2, s.AddressAt(at(e, redelegationQueueDstValidator), redelegationQueueDstValidator.offset))
	require.Equal(t, sei(2), s.Uint256(at(e, redelegationQueueInitialBalance)))
	require.Equal(t, dec.FromInt(sei(2)), s.Uint256(at(e, redelegationQueueSharesDst)))

	log := h.state.logs[len(h.state.logs)-1]
	require.Equal(t, []common.Hash{ABI.Events[RedelegateEvent].ID, addressTopic(alice), addressTopic(val1), addressTopic(val2)}, log.Topics)
	data, err := ABI.Events[RedelegateEvent].Inputs.NonIndexed().Unpack(log.Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{sei(2), completion}, data)
	h.requireInvariants(alice)
}

func TestRedelegatingFromAnUnbondedValidatorCompletesAtOnce(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val3, sei(5)))
	require.NoError(t, h.redelegate(alice, val3, val1, sei(5)))
	require.Zero(t, h.store().redelegationEntryCount(alice, val3, val1).Sign())
	q := h.store().redelegationQueue()
	require.Equal(t, q.head(), q.tail(), "no entry is queued")
	require.Equal(t, sei(55), h.state.GetBalance(BondedEscrow), "the tokens move to the bonded escrow")
	data, err := ABI.Events[RedelegateEvent].Inputs.NonIndexed().Unpack(h.state.logs[len(h.state.logs)-1].Data)
	require.NoError(t, err)
	require.Equal(t, []interface{}{sei(5), uint64(0)}, data)
	h.requireInvariants(alice)
}

func TestRedelegatingOutOfAnOpenRedelegationWaits(t *testing.T) {
	h := newHarness(t, testParams)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	require.NoError(t, h.redelegate(alice, val1, val2, sei(2)))
	require.ErrorIs(t, h.redelegate(alice, val2, val1, sei(1)), errTransitiveRedelegation)

	h.advance(testParams.UnbondingTime)
	require.Empty(t, h.failures)
	require.Zero(t, h.store().receivingRedelegations(val2, alice).Sign())
	require.NoError(t, h.redelegate(alice, val2, val1, sei(1)))
	h.requireInvariants(alice)
}

func TestRedelegateRejectsBadCalls(t *testing.T) {
	params := testParams
	params.MaxEntries = 1
	h := newHarness(t, params)
	require.NoError(t, h.delegate(alice, val1, sei(5)))
	requireReverts(t, h.redelegate(alice, val1, val2, new(big.Int)), errInvalidAmount)
	require.ErrorIs(t, h.redelegate(alice, val1, val1, sei(1)), errSelfRedelegation)
	require.ErrorIs(t, h.redelegate(alice, val1, bob, sei(1)), errBadRedelegationValidator)
	require.ErrorIs(t, h.redelegate(alice, bob, val2, sei(1)), errNoValidatorFound)
	require.ErrorIs(t, h.redelegate(alice, val1, val2, sei(6)), errInvalidShares)
	require.NoError(t, h.redelegate(alice, val1, val2, sei(1)))
	require.ErrorIs(t, h.redelegate(alice, val1, val2, sei(1)), errMaxRedelegationEntries)
	h.requireInvariants(alice)
}

func TestDelegateKeepsTheVotingPowerRatio(t *testing.T) {
	params := testParams
	params.MaxVotingPowerRatio = new(big.Int).Div(dec.One(), big.NewInt(2))
	params.MaxVotingPowerEnforcementThreshold = big.NewInt(1)
	h := newHarness(t, params)
	// val1 holds 30 of 50 bonded power; any more takes it past one half.
	require.ErrorIs(t, h.delegate(alice, val1, sei(1)), errExceedMaxVotingPower)
	require.NoError(t, h.delegate(alice, val2, sei(1)))
	h.requireInvariants(alice)
}

func TestThePowerIndexFollowsEveryChange(t *testing.T) {
	h := newHarness(t, testParams)
	require.Equal(t, []common.Address{val1, val2, val3}, h.powerIndex())
	require.NoError(t, h.delegate(alice, val3, sei(25)))
	require.Equal(t, []common.Address{val3, val1, val2}, h.powerIndex())
	require.NoError(t, h.undelegate(alice, val3, sei(16)))
	require.Equal(t, []common.Address{val1, val2, val3}, h.powerIndex())
	require.NoError(t, h.delegate(alice, val3, sei(1)))
	require.Equal(t, []common.Address{val1, val2, val3}, h.powerIndex(), "a tie goes to the lower address")
	require.NoError(t, h.delegate(bob, val3, sei(11)))
	require.Equal(t, []common.Address{val3, val1, val2}, h.powerIndex())
	require.NoError(t, h.redelegate(bob, val3, val2, sei(11)))
	require.Equal(t, []common.Address{val2, val1, val3}, h.powerIndex(), "one call moves two validators")
	h.requireInvariants(alice, bob)
}

func TestRetiredMethodsRevertNamingTheReplacement(t *testing.T) {
	h := newHarness(t, testParams)
	for sig, args := range map[string][]interface{}{
		"delegate(string)":                                           {"seivaloper1"},
		"redelegate(string,string,uint256)":                          {"seivaloper1", "seivaloper2", big.NewInt(1)},
		"undelegate(string,uint256)":                                 {"seivaloper1", big.NewInt(1)},
		"grantStakingAuthorization(address,string[],uint256,int64)":  {bob, []string{"seivaloper1"}, big.NewInt(1), int64(1)},
		"delegateWithAuthorization(address,string)":                  {bob, "seivaloper1"},
		"redelegateWithAuthorization(address,string,string,uint256)": {bob, "seivaloper1", "seivaloper2", big.NewInt(1)},
		"undelegateWithAuthorization(address,string,uint256)":        {bob, "seivaloper1", big.NewInt(1)},
		"revokeStakingAuthorization(address)":                        {bob},
	} {
		method := methodBySig(t, sig)
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, sig)
		err = h.run(alice, callOpts{}, append(method.ID, input...))
		requireReverts(t, err, errorString(retiredMethods[sig]), sig)
	}
}

func TestEveryMethodIsServedOrRetired(t *testing.T) {
	h := newHarness(t, testParams)
	for name, method := range ABI.Methods {
		if _, retired := retiredMethods[method.Sig]; retired {
			continue
		}
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
			if in.Type.T == abi.UintTy && in.Type.Size == 256 {
				args[i] = sei(1)
			}
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, name)
		require.NotErrorIs(t, h.run(alice, callOpts{}, append(method.ID, input...)), errUnknownMethod, name)
	}
}

// TestABIKeepsV2Methods pins the methods staking shares with precompiles/staking
// to V2's signatures, so callers of those selectors get a revert naming the
// call to use instead.
func TestABIKeepsV2Methods(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/staking/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	for sig := range retiredMethods {
		var found bool
		for _, m := range v2.Methods {
			found = found || m.Sig == sig
		}
		require.True(t, found, "%s is a V2 method", sig)
		require.Equal(t, sig, methodBySig(t, sig).Sig)
	}
}

func TestAddGenesisValidatorRejectsReuse(t *testing.T) {
	h := newHarness(t, testParams)
	h.fund(bob, sei(10))
	g := GenesisValidator{Operator: val1, ConsensusPubkey: common.HexToHash("0x01"), SelfDelegation: sei(1), MinSelfDelegation: sei(1)}
	require.ErrorIs(t, h.staking.AddGenesisValidator(h.block(), h.state, g), errValidatorExists)
	g.Operator, g.ConsensusPubkey = bob, common.BytesToHash(val1[:])
	require.ErrorIs(t, h.staking.AddGenesisValidator(h.block(), h.state, g), errPubkeyExists)
	g.ConsensusPubkey = common.HexToHash("0x01")
	g.SelfDelegation = big.NewInt(1)
	require.ErrorIs(t, h.staking.AddGenesisValidator(h.block(), h.state, g), errInvalidGenesis)
	require.ErrorIs(t, Precompile{}.AddGenesisValidator(h.block(), newMemState(), g), errStakingNotWritten)
}

func TestGenesisValidatorsHoldConsensusPower(t *testing.T) {
	h := newHarness(t, testParams)
	h.fund(bob, sei(1))
	g := GenesisValidator{
		Operator:          bob,
		ConsensusPubkey:   common.BytesToHash(bob[:]),
		SelfDelegation:    new(big.Int).Sub(sei(1), weiPerUsei),
		MinSelfDelegation: weiPerUsei,
	}
	require.ErrorIs(t, h.staking.AddGenesisValidator(h.block(), h.state, g), errInvalidGenesis,
		"a genesis set below one SEI each would never bond, and staking would never start")
	g.SelfDelegation = sei(1)
	require.NoError(t, h.staking.AddGenesisValidator(h.block(), h.state, g))
}

func TestDefaultParamsAreValid(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())
	state := newMemState()
	require.NoError(t, SetParams(state, DefaultParams()))
	require.True(t, HasParams(state))
	require.Equal(t, DefaultParams(), chainparams.ReadStaking(state))
}

func TestRequiredGas(t *testing.T) {
	input, err := ABI.Pack(UndelegateMethod, val1, sei(1))
	require.NoError(t, err)
	require.Equal(t, uint64(transactionGas+transactionByteGas*64), Precompile{}.RequiredGas(input))
	require.Equal(t, uint64(unknownMethodGas), Precompile{}.RequiredGas([]byte{1}))
}

func methodBySig(t *testing.T, sig string) abi.Method {
	for _, m := range ABI.Methods {
		if m.Sig == sig {
			return m
		}
	}
	t.Fatalf("no method %s", sig)
	return abi.Method{}
}

type errorString string

func (e errorString) Error() string { return string(e) }

var _ precompiles.Contract = Precompile{}
