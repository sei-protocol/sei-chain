package params

import (
	"bytes"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// testGov is x/gov's default params.
var testGov = Gov{
	MaxDepositPeriod:      172_800_000_000_000,
	VotingPeriod:          172_800_000_000_000,
	ExpeditedVotingPeriod: 86_400_000_000_000,
	MinDeposit:            new(big.Int).Mul(big.NewInt(10_000_000), weiPerUsei),
	MinExpeditedDeposit:   new(big.Int).Mul(big.NewInt(20_000_000), weiPerUsei),
	Quorum:                big.NewInt(334_000_000_000_000_000),
	Threshold:             big.NewInt(500_000_000_000_000_000),
	VetoThreshold:         big.NewInt(334_000_000_000_000_000),
	ExpeditedQuorum:       big.NewInt(667_000_000_000_000_000),
	ExpeditedThreshold:    big.NewInt(667_000_000_000_000_000),
}

func newGovState(t *testing.T) *memState {
	require.NoError(t, testGov.Validate())
	state := newMemState()
	require.NoError(t, WriteGov(state, testGov))
	return state
}

func word(v uint64) []byte {
	return common.BigToHash(new(big.Int).SetUint64(v)).Bytes()
}

func call(t *testing.T, state precompiles.State, method string, args ...interface{}) ([]interface{}, error) {
	input, err := ABI.Pack(method, args...)
	require.NoError(t, err)
	out, err := Precompile{}.Run(&precompiles.Context{Address: Address, ReadOnly: true, State: state}, input)
	if err != nil {
		return nil, err
	}
	return ABI.Methods[method].Outputs.Unpack(out)
}

// TestKeys pins the declared params. The flat keys of one V2 key stay adjacent
// and in the V2 struct's field order; a new param goes in its place in that order.
func TestKeys(t *testing.T) {
	require.Equal(t, []Key{
		{"gov", "depositparams.min_deposit", "uint256"},
		{"gov", "depositparams.max_deposit_period", "uint64"},
		{"gov", "depositparams.min_expedited_deposit", "uint256"},
		{"gov", "votingparams.voting_period", "uint64"},
		{"gov", "votingparams.expedited_voting_period", "uint64"},
		{"gov", "tallyparams.quorum", "uint256"},
		{"gov", "tallyparams.threshold", "uint256"},
		{"gov", "tallyparams.veto_threshold", "uint256"},
		{"gov", "tallyparams.expedited_quorum", "uint256"},
		{"gov", "tallyparams.expedited_threshold", "uint256"},
	}, Keys())
}

func TestParamReturnsTheStoredValue(t *testing.T) {
	state := newGovState(t)
	out, err := call(t, state, ParamMethod, "gov", "votingparams.voting_period")
	require.NoError(t, err)
	require.Equal(t, word(testGov.VotingPeriod), out[0])

	_, err = call(t, state, ParamMethod, "gov", "votingparams")
	require.ErrorIs(t, err, errUnknownKey)
	_, err = call(t, state, ParamMethod, "baseapp", "TimeoutParams")
	require.ErrorIs(t, err, errUnknownSubspace)
}

func TestApplyWritesEachChange(t *testing.T) {
	state := newGovState(t)
	require.NoError(t, Apply(state, []Change{
		{"gov", "votingparams.voting_period", word(200_000_000_000_000)},
		{"gov", "depositparams.min_deposit", word(5_000_000_000_000)},
		{"gov", "votingparams.voting_period", word(300_000_000_000_000)},
	}))
	want := testGov
	want.VotingPeriod = 300_000_000_000_000
	want.MinDeposit = big.NewInt(5_000_000_000_000)
	require.Equal(t, want, ReadGov(state), "later changes win")
}

func TestApplyRejectsAndWritesNothing(t *testing.T) {
	for name, changes := range map[string][]Change{
		"none":                {},
		"undeclared subspace": {{"baseapp", "TimeoutParams", word(1)}},
		"undeclared key":      {{"gov", "voting_period", word(1)}},
		"short value":         {{"gov", "votingparams.voting_period", word(1)[1:]}},
		"duration overflow":   {{"gov", "votingparams.voting_period", common.BigToHash(new(big.Int).Lsh(big.NewInt(1), 64)).Bytes()}},
		"zero voting period":  {{"gov", "votingparams.voting_period", word(0)}},
		"quorum above one":    {{"gov", "tallyparams.quorum", common.BigToHash(new(big.Int).Add(decOne, big.NewInt(1))).Bytes()}},
		"expedited deposit":   {{"gov", "depositparams.min_expedited_deposit", common.BigToHash(testGov.MinDeposit).Bytes()}},
		"partial usei":        {{"gov", "depositparams.min_deposit", word(5_000_000_000_001)}},
		"expedited threshold": {{"gov", "tallyparams.expedited_threshold", word(500_000_000_000_000_000)}},
		"huge deposit":        {{"gov", "depositparams.min_expedited_deposit", common.MaxHash.Bytes()}},
		"valid then invalid": {
			{"gov", "votingparams.voting_period", word(200_000_000_000_000)},
			{"gov", "votingparams.expedited_voting_period", word(200_000_000_000_000)},
		},
	} {
		state := newGovState(t)
		require.Error(t, Apply(state, changes), name)
		require.Equal(t, testGov, ReadGov(state), name)
	}
}

func TestGovUpperBounds(t *testing.T) {
	sei := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000_000_000)) }
	ratio := func(v int64) *big.Int { return big.NewInt(v) }
	for name, set := range map[string]func(g *Gov, over bool){
		"voting_period": func(g *Gov, over bool) {
			g.VotingPeriod = MaxVotingPeriod
			if over {
				g.VotingPeriod++
			}
		},
		"min_deposit": func(g *Gov, over bool) {
			g.MinDeposit, g.MinExpeditedDeposit = sei(1_000_000), sei(2_000_000)
			if over {
				g.MinDeposit = new(big.Int).Add(sei(1_000_000), weiPerUsei)
			}
		},
		"quorum": func(g *Gov, over bool) {
			g.Quorum, g.ExpeditedQuorum = ratio(800_000_000_000_000_000), ratio(900_000_000_000_000_000)
			if over {
				g.Quorum = ratio(800_000_000_000_000_001)
			}
		},
		"threshold": func(g *Gov, over bool) {
			g.Threshold, g.ExpeditedThreshold = ratio(800_000_000_000_000_000), ratio(900_000_000_000_000_000)
			if over {
				g.Threshold = ratio(800_000_000_000_000_001)
			}
		},
	} {
		at, over := testGov, testGov
		set(&at, false)
		set(&over, true)
		require.NoError(t, at.Validate(), name)
		require.ErrorContains(t, over.Validate(), name+" must be at most", name)
	}
}

func TestSetLeavesTheRestOfTheSlot(t *testing.T) {
	uint32Kind := valueKind{solType: "uint32", bits: 32}
	ss := &subspace{name: "packed", slot: 99, params: []param{
		{key: "a", kind: uint32Kind, at: member{0, 0}},
		{key: "b", kind: uint32Kind, at: member{0, 4}},
		{key: "c", kind: durationKind, at: member{0, 8}},
		{key: "d", kind: decKind, at: member{1, 0}},
	}, check: func(values) error { return nil }}
	s := newStore(newMemState())
	for i, p := range ss.params {
		ss.set(s, p, big.NewInt(int64(i+1)))
	}
	for i, p := range ss.params {
		before := ss.read(s)
		ss.set(s, p, big.NewInt(int64(100+i)))
		after := ss.read(s)
		for _, q := range ss.params {
			if q.key == p.key {
				require.Equal(t, int64(100+i), after[q.key].Int64(), q.key)
			} else {
				require.Equal(t, before[q.key], after[q.key], "setting %s changed %s", p.key, q.key)
			}
		}
	}
}

func TestEveryGovParamWritesOnlyItself(t *testing.T) {
	for _, p := range govSubspace.params {
		state := newGovState(t)
		before := govSubspace.read(newStore(state))
		govSubspace.set(newStore(state), p, big.NewInt(7))
		after := govSubspace.read(newStore(state))
		for _, q := range govSubspace.params {
			if q.key != p.key {
				require.Equal(t, before[q.key], after[q.key], "setting %s changed %s", p.key, q.key)
			}
		}
	}
}

func TestGovValuesRoundTrip(t *testing.T) {
	v := testGov.values()
	require.Len(t, v, len(govSubspace.params), "values() has one entry per declared gov param")
	for _, p := range govSubspace.params {
		require.Contains(t, v, p.key)
	}
	require.Equal(t, testGov, govFrom(v), "Gov, the table, govFrom and values() agree")
}

func TestWriteGovRejectsAValueThatDoesNotFit(t *testing.T) {
	g := testGov
	g.MinDeposit = new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), testGov.MinDeposit)
	state := newMemState()
	require.ErrorIs(t, WriteGov(state, g), errInvalidValue)
	require.False(t, HasGov(state), "nothing is written")
}

func TestUnknownNamesAreQuotedAndCapped(t *testing.T) {
	long := strings.Repeat("x", 10_000) + "\n"
	_, err := plan(newGovState(t), []Change{{long, "k", word(1)}})
	require.ErrorIs(t, err, errUnknownSubspace)
	require.Less(t, len(err.Error()), 200)
	_, err = plan(newGovState(t), []Change{{"gov", long, word(1)}})
	require.ErrorIs(t, err, errUnknownKey)
	require.Less(t, len(err.Error()), 200)
	_, err = call(t, newGovState(t), ParamMethod, long, long)
	require.ErrorIs(t, err, errUnknownSubspace)
	require.Less(t, len(err.Error()), 200)
}

func TestApplyChecksTheResultAsAWhole(t *testing.T) {
	state := newGovState(t)
	// Lowering the voting period below the expedited one fails alone and
	// passes with the expedited period lowered too.
	shorter := Change{"gov", "votingparams.voting_period", word(600_000_000_000)}
	require.Error(t, Check(state, []Change{shorter}))
	require.NoError(t, Apply(state, []Change{shorter, {"gov", "votingparams.expedited_voting_period", word(300_000_000_000)}}))
	require.Equal(t, uint64(600_000_000_000), ReadGov(state).VotingPeriod)
}

func TestApplyWritesOnlyEachParamsLastValue(t *testing.T) {
	state := newGovState(t)
	oversized := common.BigToHash(new(big.Int).Lsh(big.NewInt(1), 64)).Bytes()
	require.NoError(t, Apply(state, []Change{
		{"gov", "votingparams.voting_period", oversized},
		{"gov", "votingparams.voting_period", word(200_000_000_000_000)},
	}))
	want := testGov
	want.VotingPeriod = 200_000_000_000_000
	require.Equal(t, want, ReadGov(state))
}

func TestChangesKeepTheVotingPeriodFloor(t *testing.T) {
	state := newGovState(t)
	floor := Change{"gov", "votingparams.voting_period", word(MinChangedVotingPeriod)}
	expedited := Change{"gov", "votingparams.expedited_voting_period", word(MinChangedVotingPeriod / 2)}
	below := Change{"gov", "votingparams.voting_period", word(MinChangedVotingPeriod - 1)}
	require.ErrorIs(t, Check(state, []Change{below, expedited}), errInvalidValue)
	require.ErrorIs(t, Check(state, []Change{floor, below, expedited}), errInvalidValue, "the last value counts")
	require.NoError(t, Apply(state, []Change{below, floor, expedited}))
	require.Equal(t, MinChangedVotingPeriod, ReadGov(state).VotingPeriod)

	short := testGov
	short.VotingPeriod = 30_000_000_000
	short.ExpeditedVotingPeriod = 15_000_000_000
	require.NoError(t, WriteGov(newMemState(), short), "genesis may set a voting period below the floor")
}

func TestRunRejectsValueAndDelegateCall(t *testing.T) {
	input, err := ABI.Pack(ParamMethod, "gov", "votingparams.voting_period")
	require.NoError(t, err)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState(), ApparentValue: big.NewInt(1)}, input)
	require.ErrorIs(t, err, errNonPayable)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState(), DelegateCall: true}, input)
	require.ErrorIs(t, err, errDelegateCall)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState()}, []byte{1, 2, 3})
	require.ErrorIs(t, err, errUnknownMethod)
}

func TestRetiredParamsMethodReverts(t *testing.T) {
	method := ABI.Methods["params"]
	input, err := method.Inputs.Pack("gov", "votingparams")
	require.NoError(t, err)
	out, err := Precompile{}.Run(&precompiles.Context{Address: Address, State: newGovState(t)}, append(method.ID, input...))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(out)
	require.NoError(t, err)
	require.Equal(t, retiredMethods["params(string,string)"], reason)
}

func TestEveryMethodIsServedOrRetired(t *testing.T) {
	for name, method := range ABI.Methods {
		require.Equal(t, method.RawName, name, "no overload is renamed by go-ethereum")
		if _, retired := retiredMethods[method.Sig]; retired {
			continue
		}
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, name)
		_, err = Precompile{}.Run(&precompiles.Context{Address: Address, State: newGovState(t)}, append(method.ID, input...))
		require.NotErrorIs(t, err, errUnknownMethod, name)
	}
}

func TestHasGov(t *testing.T) {
	require.False(t, HasGov(newMemState()))
	require.True(t, HasGov(newGovState(t)))
}

func TestABIKeepsV2Methods(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/params/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	require.NotEmpty(t, v2.Methods)
	for _, m := range v2.Methods {
		require.Contains(t, retiredMethods, m.Sig)
		require.Equal(t, m.ID, ABI.Methods[m.Name].ID, m.Sig)
	}
}
