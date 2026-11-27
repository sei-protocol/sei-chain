package distribution

import (
	"bytes"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

func TestWithdrawDelegationRewards(t *testing.T) {
	h := newHarness(t)
	h.delegate(alice, val1, ether(300))
	h.settle(val1, ether(40))

	h.mustCall(alice, WithdrawDelegationRewardsMethod, val1)
	require.Equal(t, ether(30), h.balance(alice))
	require.Zero(t, h.owed(alice, val1).Sign())
	require.Equal(t, ether(10), h.store().outstandingRewards(val1), "val1's own share is still owed")
	requireLastLog(t, h, DelegationRewardsWithdrawnEvent, []common.Hash{addressTopic(alice), addressTopic(val1)}, ether(30))

	h.mustCall(alice, WithdrawDelegationRewardsMethod, val1)
	require.Equal(t, ether(30), h.balance(alice), "nothing more is owed")
	h.requireBacked(val1, val2)

	_, err := h.call(alice, WithdrawDelegationRewardsMethod, val2)
	require.ErrorIs(t, err, errNoStartingInfo)
}

func TestWithdrawMultipleDelegationRewards(t *testing.T) {
	h := newHarness(t)
	h.stake(alice, ether(300), staking.DelegateMethod, val1)
	h.stake(alice, ether(100), staking.DelegateMethod, val2)
	h.applyStaged()
	h.settle(val1, ether(40))
	h.settle(val2, ether(20))

	logs := len(h.state.logs)
	h.mustCall(alice, WithdrawMultipleDelegationRewardsMethod, []common.Address{val1, val2})
	require.Equal(t, ether(40), h.balance(alice), "30 from val1 and 10 from val2")
	require.Len(t, h.state.logs, logs+2, "one event a validator")

	_, err := h.call(alice, WithdrawMultipleDelegationRewardsMethod, []common.Address{val1, bob})
	require.ErrorIs(t, err, errNoStartingInfo)
	require.Equal(t, ether(40), h.balance(alice), "a failed withdrawal pays nothing")
}

func TestWithdrawalArgumentsAreRangeChecked(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method string
		arg    interface{}
		reason string
	}{
		{WithdrawDelegationRewardsMethod, common.Address{}, "validator: address must not be the zero address"},
		{WithdrawMultipleDelegationRewardsMethod, []common.Address{}, "validators must not be empty"},
		{WithdrawMultipleDelegationRewardsMethod, []common.Address{val1, {}}, "validators: address must not be the zero address"},
		{WithdrawMultipleDelegationRewardsMethod, []common.Address{val1, val1}, "validators must not repeat: " + val1.Hex()},
		{SetWithdrawAddressMethod, common.Address{}, "withdrawAddr: address must not be the zero address"},
	} {
		_, err := h.call(alice, tc.method, tc.arg)
		requireReverts(t, err, tc.reason)
	}
}

func TestSetWithdrawAddress(t *testing.T) {
	h := newHarness(t)
	h.delegate(alice, val1, ether(300))
	h.settle(val1, ether(40))

	h.mustCall(alice, SetWithdrawAddressMethod, carol)
	requireLastLog(t, h, WithdrawAddressSetEvent, []common.Hash{addressTopic(alice)}, carol)
	require.Equal(t, []interface{}{carol}, h.query(DelegatorWithdrawAddressMethod, alice))
	h.mustCall(alice, WithdrawDelegationRewardsMethod, val1)
	require.Equal(t, ether(30), h.balance(carol))
	require.Zero(t, h.balance(alice).Sign())

	h.mustCall(alice, SetWithdrawAddressMethod, alice)
	require.Equal(t, []interface{}{alice}, h.query(DelegatorWithdrawAddressMethod, alice))
	require.Equal(t, common.Address{}, h.store().AddressAt(withdrawAddrSlot(alice), 0), "the delegator itself is stored as no record")

	for _, blocked := range []common.Address{DistributionEscrow, FeeCollector, staking.BondedEscrow, staking.NotBondedEscrow, staking.StagedEscrow} {
		_, err := h.call(alice, SetWithdrawAddressMethod, blocked)
		require.ErrorIs(t, err, errBlockedWithdrawAddr, blocked.Hex())
	}

	require.NoError(t, chainparams.Apply(h.state, []chainparams.Change{{Subspace: "distribution", Key: "withdrawaddrenabled", Value: common.Hash{}.Bytes()}}))
	_, err := h.call(alice, SetWithdrawAddressMethod, carol)
	require.ErrorIs(t, err, errWithdrawAddrDisabled)
}

func TestWithdrawValidatorCommission(t *testing.T) {
	h := newHarness(t)
	h.settleAt(val1, ether(100), pct(10))
	require.Equal(t, []interface{}{ether(10)}, h.query(ValidatorCommissionMethod, val1))
	require.Equal(t, []interface{}{ether(100)}, h.query(ValidatorOutstandingRewardsMethod, val1))

	h.mustCall(val1, WithdrawValidatorCommissionMethod)
	require.Equal(t, ether(10), h.balance(val1))
	requireLastLog(t, h, ValidatorCommissionWithdrawnEvent, []common.Hash{addressTopic(val1)}, ether(10))
	require.Zero(t, h.query(ValidatorCommissionMethod, val1)[0].(*big.Int).Sign())
	require.Equal(t, []interface{}{ether(90)}, h.query(ValidatorOutstandingRewardsMethod, val1))
	h.requireBacked(val1, val2)

	_, err := h.call(val1, WithdrawValidatorCommissionMethod)
	require.ErrorIs(t, err, errNoValidatorCommission)

	h.mustCall(val1, SetWithdrawAddressMethod, carol)
	h.settleAt(val1, ether(100), pct(10))
	h.mustCall(val1, WithdrawValidatorCommissionMethod)
	require.Equal(t, ether(10), h.balance(carol), "commission goes to the operator's withdraw address")
}

func TestParamsQuery(t *testing.T) {
	h := newHarness(t)
	out := h.query(ParamsMethod)[0].(struct {
		WithdrawAddrEnabled bool           `json:"withdrawAddrEnabled"`
		DaoAddress          common.Address `json:"daoAddress"`
	})
	require.True(t, out.WithdrawAddrEnabled)
	require.Equal(t, dao, out.DaoAddress)
}

func TestRewardsQueryCoversEveryDelegation(t *testing.T) {
	h := newHarness(t)
	h.stake(alice, ether(300), staking.DelegateMethod, val1)
	h.stake(alice, ether(100), staking.DelegateMethod, val2)
	h.applyStaged()
	h.settle(val1, ether(40))

	got := toRewards(h.query(RewardsMethod, alice))[0].(Rewards)
	require.Equal(t, ether(30), got.Total)
	require.Len(t, got.Rewards, 2)
	require.Equal(t, DelegationReward{Validator: val1, Amount: ether(30)}, got.Rewards[0])
	require.Equal(t, val2, got.Rewards[1].Validator)
	require.Zero(t, got.Rewards[1].Amount.Sign())

	empty := toRewards(h.query(RewardsMethod, bob))[0].(Rewards)
	require.Empty(t, empty.Rewards)
	require.Zero(t, empty.Total.Sign())

	_, err := h.call(common.Address{}, DelegationRewardsMethod, bob, val1)
	require.ErrorIs(t, err, errNoStartingInfo)
}

func TestCallsAreGuarded(t *testing.T) {
	h := newHarness(t)
	input, err := ABI.Pack(WithdrawValidatorCommissionMethod)
	require.NoError(t, err)
	_, err = h.run(val1, callOpts{delegateCall: true}, input)
	require.ErrorIs(t, err, errDelegateCall)
	_, err = h.run(val1, callOpts{readOnly: true}, input)
	require.ErrorIs(t, err, errStaticCall)
	_, err = h.run(val1, callOpts{value: big.NewInt(1)}, input)
	require.ErrorIs(t, err, errNonPayable)
	_, err = h.run(val1, callOpts{}, []byte{1, 2, 3})
	require.ErrorIs(t, err, errUnknownMethod)
}

func TestRetiredMethodsRevertNamingTheCallToUse(t *testing.T) {
	h := newHarness(t)
	for sig, reason := range retiredMethods {
		method := methodBySig(t, sig)
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, sig)
		_, err = h.run(alice, callOpts{}, append(method.ID, input...))
		requireReverts(t, err, reason)
	}
}

func TestEveryMethodIsServedOrRetired(t *testing.T) {
	h := newHarness(t)
	for name, method := range ABI.Methods {
		if _, retired := retiredMethods[method.Sig]; retired {
			continue
		}
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, name)
		_, err = h.run(alice, callOpts{readOnly: method.IsConstant()}, append(method.ID, input...))
		require.NotErrorIs(t, err, errUnknownMethod, name)
	}
}

// TestABIKeepsV2Methods requires every method of precompiles/distribution to
// keep its selector here, served or reverting with the call to use instead.
func TestABIKeepsV2Methods(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/distribution/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	for _, m := range v2.Methods {
		require.Equal(t, m.Sig, methodBySig(t, m.Sig).Sig)
	}
	for sig := range retiredMethods {
		var found bool
		for _, m := range v2.Methods {
			found = found || m.Sig == sig
		}
		require.True(t, found, "%s is a V2 method", sig)
	}
}

func TestRequiredGas(t *testing.T) {
	input, err := ABI.Pack(WithdrawDelegationRewardsMethod, val1)
	require.NoError(t, err)
	require.Equal(t, uint64(transactionGas+transactionByteGas*32), Precompile{}.RequiredGas(input))
	input, err = ABI.Pack(DelegationRewardsMethod, alice, val1)
	require.NoError(t, err)
	require.Equal(t, uint64(queryGas+queryByteGas*64), Precompile{}.RequiredGas(input))
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

// requireReverts requires err to be a revert whose reason is reason.
func requireReverts(t *testing.T, err error, reason string) {
	t.Helper()
	require.EqualError(t, err, "execution reverted: "+reason)
}

// requireLastLog requires the last log to be event name with the topics after
// its ID, and data.
func requireLastLog(t *testing.T, h *harness, name string, topics []common.Hash, data ...interface{}) {
	t.Helper()
	require.NotEmpty(t, h.state.logs)
	l := h.state.logs[len(h.state.logs)-1]
	event := ABI.Events[name]
	require.Equal(t, Address, l.Address)
	require.Equal(t, append([]common.Hash{event.ID}, topics...), l.Topics)
	want, err := event.Inputs.NonIndexed().Pack(data...)
	require.NoError(t, err)
	require.Equal(t, want, l.Data)
}
