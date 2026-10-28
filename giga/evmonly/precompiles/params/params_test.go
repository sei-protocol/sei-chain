package params

import (
	"encoding/json"
	"math/big"
	"testing"

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

func legacyParams(t *testing.T, state precompiles.State, subspace, key string) string {
	out, err := call(t, state, ParamsMethod, subspace, key)
	require.NoError(t, err)
	return out[0].(string)
}

func TestDurationJSON(t *testing.T) {
	for in, nanos := range map[string]uint64{
		`"259200000000000"`: 259_200_000_000_000,
		`"0"`:               0,
		`"259200s"`:         259_200_000_000_000,
		`"0.5s"`:            500_000_000,
		`"1.000000001s"`:    1_000_000_001,
		`"1500ms"`:          1_500_000_000,
		`"0.000001ms"`:      1,
		`"7ns"`:             7,
	} {
		v, err := parseDuration(json.RawMessage(in))
		require.NoError(t, err, in)
		require.Equal(t, new(big.Int).SetUint64(nanos), v, in)
	}
	for _, in := range []string{`259200000000000`, `""`, `"-1"`, `"+1"`, `"1.5"`, `"s"`, `"1.s"`, `".5s"`, `"1.0000000001s"`, `"0.0000001ms"`, `"1.5ns"`, `"1us"`, `"1µs"`, `"1sms"`, `"1S"`, `"1 s"`, `"1e9"`, `"1m"`, `"1h"`} {
		_, err := parseDuration(json.RawMessage(in))
		require.Error(t, err, in)
	}
	for nanos, out := range map[uint64]string{
		259_200_000_000_000: "259200000000000",
		1_000_000_001:       "1000000001",
		0:                   "0",
	} {
		require.Equal(t, out, formatDuration(new(big.Int).SetUint64(nanos)))
	}
}

func TestDecJSON(t *testing.T) {
	for in, v := range map[string]uint64{
		`"0.334000000000000000"`: 334_000_000_000_000_000,
		`"0.5"`:                  500_000_000_000_000_000,
		`"1"`:                    1_000_000_000_000_000_000,
	} {
		got, err := parseDec(json.RawMessage(in))
		require.NoError(t, err, in)
		require.Equal(t, new(big.Int).SetUint64(v), got, in)
	}
	for _, in := range []string{`0.5`, `".5"`, `"-0.5"`, `"0.5000000000000000001"`, `"1."`, `""`} {
		_, err := parseDec(json.RawMessage(in))
		require.Error(t, err, in)
	}
	require.Equal(t, "0.334000000000000000", formatDec(big.NewInt(334_000_000_000_000_000)))
	require.Equal(t, "0.000000000000000001", formatDec(big.NewInt(1)))
}

func TestCoinsJSON(t *testing.T) {
	v, err := parseCoins(json.RawMessage(`[{"denom":"usei","amount":"3500000000"}]`))
	require.NoError(t, err)
	require.Equal(t, big.NewInt(3_500_000_000), v)
	v, err = parseCoins(json.RawMessage(`[]`))
	require.NoError(t, err)
	require.Zero(t, v.Sign())
	for _, in := range []string{
		`{"denom":"usei","amount":"1"}`,
		`[{"denom":"uatom","amount":"1"}]`,
		`[{"denom":"usei","amount":"1"},{"denom":"usei","amount":"1"}]`,
		`[{"denom":"usei","amount":1}]`,
		`[{"denom":"usei","amount":"-1"}]`,
		`[{"denom":"usei","amount":"1","extra":true}]`,
		`[{"denom":"usei","amount":"1"}] []`,
	} {
		_, err := parseCoins(json.RawMessage(in))
		require.Error(t, err, in)
	}
}

func TestParamKeys(t *testing.T) {
	out, err := call(t, newMemState(), ParamKeysMethod)
	require.NoError(t, err)
	var keys []ParamKey
	b, err := json.Marshal(out[0])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &keys))
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

func TestParamsReturnsAminoJSON(t *testing.T) {
	state := newGovState(t)
	require.Equal(t, `{"min_deposit":[{"denom":"usei","amount":"10000000"}],"max_deposit_period":"172800000000000","min_expedited_deposit":[{"denom":"usei","amount":"20000000"}]}`,
		legacyParams(t, state, "gov", "depositparams"))
	require.Equal(t, `{"voting_period":"172800000000000","expedited_voting_period":"86400000000000"}`,
		legacyParams(t, state, "gov", "votingparams"))
	require.Equal(t, `{"quorum":"0.334000000000000000","threshold":"0.500000000000000000","veto_threshold":"0.334000000000000000","expedited_quorum":"0.667000000000000000","expedited_threshold":"0.667000000000000000"}`,
		legacyParams(t, state, "gov", "tallyparams"))
	require.Equal(t, "", legacyParams(t, state, "gov", "votingparams.voting_period"), "an undeclared V2 key is empty")
	_, err := call(t, state, ParamsMethod, "baseapp", "TimeoutParams")
	require.ErrorIs(t, err, errUnknownSubspace)
	require.Equal(t, `{}`, legacyParams(t, newMemState(), "gov", "votingparams"), "amino omits empty fields")
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

// Mainnet (pacific-1) ParameterChange proposals, with each change's value as
// the chain stores it.
var (
	proposal16 = []LegacyChange{{Subspace: "gov", Key: "depositparams", Value: json.RawMessage("{\n          \"min_deposit\": [{\n            \"amount\": \"3500000000\",\n            \"denom\": \"usei\"\n          }],\n\t  \"min_expedited_deposit\": [{\n            \"amount\": \"7000000000\",\n            \"denom\": \"usei\"\n          }]\n        }")}}
	proposal86 = []LegacyChange{{Subspace: "gov", Key: "votingparams", Value: json.RawMessage("{\n           \"voting_period\": \"259200000000000\"\n         }")}}
	// proposal123 changes consensus timeouts, which have no evmonly param.
	proposal123 = []LegacyChange{{Subspace: "baseapp", Key: "TimeoutParams", Value: json.RawMessage("{\n        \"propose\": \"300000000\",\n        \"propose_delta\": \"50000000\",\n        \"vote\": \"50000000\",\n        \"vote_delta\": \"50000000\",\n        \"commit\": \"200000000\",\n        \"bypass_commit_timeout\": false\n      }")}}
)

func TestMainnetProposals(t *testing.T) {
	state := newGovState(t)
	for _, proposal := range [][]LegacyChange{proposal16, proposal86} {
		changes, err := FromLegacy(proposal)
		require.NoError(t, err)
		require.NoError(t, Apply(state, changes))
	}
	require.Equal(t, `{"min_deposit":[{"denom":"usei","amount":"3500000000"}],"max_deposit_period":"172800000000000","min_expedited_deposit":[{"denom":"usei","amount":"7000000000"}]}`,
		legacyParams(t, state, "gov", "depositparams"))
	require.Equal(t, `{"voting_period":"259200000000000","expedited_voting_period":"86400000000000"}`,
		legacyParams(t, state, "gov", "votingparams"))

	_, err := FromLegacy(proposal123)
	require.ErrorIs(t, err, errUnknownSubspace)
}

func TestFromLegacySplitsFieldsInDeclaredOrder(t *testing.T) {
	changes, err := FromLegacy([]LegacyChange{{
		Subspace: "gov", Key: "tallyparams",
		Value: json.RawMessage(`{"threshold":"0.6","quorum":"0.4"}`),
	}})
	require.NoError(t, err)
	require.Equal(t, []Change{
		{"gov", "tallyparams.quorum", word(400_000_000_000_000_000)},
		{"gov", "tallyparams.threshold", word(600_000_000_000_000_000)},
	}, changes)
}

func TestFromLegacyRejects(t *testing.T) {
	for name, lc := range map[string]LegacyChange{
		"undeclared key":      {Subspace: "gov", Key: "params", Value: json.RawMessage(`{}`)},
		"flat key":            {Subspace: "gov", Key: "votingparams.voting_period", Value: json.RawMessage(`"1"`)},
		"unknown field":       {Subspace: "gov", Key: "votingparams", Value: json.RawMessage(`{"voting_periods":"1"}`)},
		"duplicate field":     {Subspace: "gov", Key: "votingparams", Value: json.RawMessage(`{"voting_period":"1","voting_period":"2"}`)},
		"not an object":       {Subspace: "gov", Key: "votingparams", Value: json.RawMessage(`"{\"voting_period\":\"1\"}"`)},
		"trailing data":       {Subspace: "gov", Key: "votingparams", Value: json.RawMessage(`{"voting_period":"1"} {}`)},
		"bare duration":       {Subspace: "gov", Key: "votingparams", Value: json.RawMessage(`{"voting_period":1}`)},
		"duration overflow":   {Subspace: "gov", Key: "votingparams", Value: json.RawMessage(`{"voting_period":"18446744073709551616"}`)},
		"bare dec":            {Subspace: "gov", Key: "tallyparams", Value: json.RawMessage(`{"quorum":0.5}`)},
		"other denom":         {Subspace: "gov", Key: "depositparams", Value: json.RawMessage(`{"min_deposit":[{"denom":"uatom","amount":"1"}]}`)},
		"undeclared subspace": {Subspace: "staking", Key: "MaxValidators", Value: json.RawMessage(`40`)},
	} {
		_, err := FromLegacy([]LegacyChange{lc})
		require.Error(t, err, name)
	}
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

func TestToV2GroupsConsecutiveFieldsOfAKey(t *testing.T) {
	require.Equal(t, []V2Change{
		{Subspace: "gov", Key: "votingparams", Value: `{"expedited_voting_period":"2","voting_period":"3"}`},
		{Subspace: "gov", Key: "tallyparams", Value: `{"quorum":"0.000000000000000001"}`},
		{Subspace: "gov", Key: "votingparams", Value: `{"voting_period":"4"}`},
		{Subspace: "gov", Key: "gone.field", Value: "0x05"},
	}, ToV2([]Change{
		{Subspace: "gov", Key: "votingparams.voting_period", Value: word(1)},
		{Subspace: "gov", Key: "votingparams.expedited_voting_period", Value: word(2)},
		{Subspace: "gov", Key: "votingparams.voting_period", Value: word(3)},
		{Subspace: "gov", Key: "tallyparams.quorum", Value: word(1)},
		{Subspace: "gov", Key: "votingparams.voting_period", Value: word(4)},
		{Subspace: "gov", Key: "gone.field", Value: []byte{5}},
	}))
}
