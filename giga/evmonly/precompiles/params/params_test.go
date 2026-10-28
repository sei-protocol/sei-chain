package params

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// testGov is x/gov's default params.
var testGov = Gov{
	MaxDepositPeriod:      172_800_000_000_000,
	VotingPeriod:          172_800_000_000_000,
	ExpeditedVotingPeriod: 86_400_000_000_000,
	MinDeposit:            big.NewInt(10_000_000),
	MinExpeditedDeposit:   big.NewInt(20_000_000),
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

func TestParamKeys(t *testing.T) {
	out, err := call(t, newMemState(), ParamKeysMethod)
	require.NoError(t, err)
	var keys []ParamKey
	abi.ConvertType(out[0], &keys)
	require.Equal(t, []ParamKey{
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
	}, keys)
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
		{"gov", "depositparams.min_deposit", word(5)},
		{"gov", "votingparams.voting_period", word(300_000_000_000_000)},
	}))
	want := testGov
	want.VotingPeriod = 300_000_000_000_000
	want.MinDeposit = big.NewInt(5)
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
		"expedited deposit":   {{"gov", "depositparams.min_expedited_deposit", word(10_000_000)}},
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

func TestApplyChecksTheResultAsAWhole(t *testing.T) {
	state := newGovState(t)
	// Lowering the voting period below the expedited one fails alone and
	// passes with the expedited period lowered too.
	shorter := Change{"gov", "votingparams.voting_period", word(60_000_000_000)}
	require.Error(t, Check(state, []Change{shorter}))
	require.NoError(t, Apply(state, []Change{shorter, {"gov", "votingparams.expedited_voting_period", word(30_000_000_000)}}))
	require.Equal(t, uint64(60_000_000_000), ReadGov(state).VotingPeriod)
}

func TestRunRejectsValueAndDelegateCall(t *testing.T) {
	input, err := ABI.Pack(ParamKeysMethod)
	require.NoError(t, err)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState(), ApparentValue: big.NewInt(1)}, input)
	require.ErrorIs(t, err, errNonPayable)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState(), DelegateCall: true}, input)
	require.ErrorIs(t, err, errDelegateCall)
	_, err = Precompile{}.Run(&precompiles.Context{State: newMemState()}, []byte{1, 2, 3})
	require.ErrorIs(t, err, errUnknownMethod)
}
