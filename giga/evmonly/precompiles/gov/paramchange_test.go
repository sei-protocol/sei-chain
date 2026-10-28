package gov

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

// paramChange is the ABI tuple submitParamChangeProposal takes.
type paramChange struct {
	Subspace string
	Key      string
	Value    []byte
}

func uintValue(v uint64) []byte {
	return common.BigToHash(new(big.Int).SetUint64(v)).Bytes()
}

func secondsValue(s uint64) []byte {
	return uintValue(s * second)
}

func (h *harness) submitParamChange(expedited bool, changes ...paramChange) (uint64, error) {
	values, err := h.call(alice, callOpts{value: usei(10)}, SubmitParamChangeProposalMethod, "t", "d", expedited, changes)
	if err != nil {
		return 0, err
	}
	return values[0].(uint64), nil
}

// passID votes proposal id through and ends its voting period.
func (h *harness) passID(id uint64) {
	for _, voter := range []common.Address{alice, bob, carol} {
		h.vote(voter, id, OptionYes)
	}
	h.time += newStore(h.state).params().VotingPeriod
	h.endBlock(1)
}

func TestTypedParamChangeAppliesWhenPassed(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitParamChange(false,
		paramChange{"gov", "votingparams.voting_period", secondsValue(300)},
		paramChange{"gov", "tallyparams.quorum", uintValue(400_000_000_000_000_000)},
	)
	require.NoError(t, err)
	require.Equal(t, testParams, newStore(h.state).params(), "nothing changes before the proposal passes")

	h.passID(id)
	require.Equal(t, StatusPassed, h.status(id))
	want := testParams
	want.VotingPeriod = 300 * second
	want.Quorum = 400_000_000_000_000_000
	require.Equal(t, want, newStore(h.state).params())

	next := h.open(textProposal, usei(10))
	require.Equal(t, h.time+300*second, h.proposal(next).VotingEndTime, "later proposals use the new voting period")
}

func TestParamChangeContentIsStored(t *testing.T) {
	h := newHarness(t)
	changes := []paramChange{
		{"gov", "votingparams.voting_period", secondsValue(300)},
		{"gov", "tallyparams.quorum", uintValue(400_000_000_000_000_000)},
	}
	id, err := h.submitParamChange(true, changes...)
	require.NoError(t, err)
	got := h.proposal(id)
	require.True(t, got.IsExpedited)
	content, err := decodeContent(got.ContentType, got.Content)
	require.NoError(t, err)
	require.Equal(t, paramChangeContent{Title: "t", Description: "d", IsExpedited: true, Changes: []chainparams.Change{
		chainparams.Change(changes[0]), chainparams.Change(changes[1]),
	}}, content)
}

func TestParamChangeFailingAtExecutionChangesNothing(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	shorter, err := h.submitParamChange(false, paramChange{"gov", "votingparams.voting_period", secondsValue(30)})
	require.NoError(t, err)
	longer, err := h.submitParamChange(false, paramChange{"gov", "votingparams.expedited_voting_period", secondsValue(40)})
	require.NoError(t, err, "valid against the params at submission")
	for _, id := range []uint64{shorter, longer} {
		for _, voter := range []common.Address{alice, bob, carol} {
			h.vote(voter, id, OptionYes)
		}
	}
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.Equal(t, StatusPassed, h.status(shorter))
	require.Equal(t, StatusFailed, h.status(longer), "expedited period is no longer below the voting period")
	want := testParams
	want.VotingPeriod = 30 * second
	require.Equal(t, want, newStore(h.state).params())
}

func TestSubmitRejectsInvalidParamChanges(t *testing.T) {
	h := newHarness(t)
	for name, changes := range map[string][]paramChange{
		"none":                {},
		"undeclared subspace": {{"baseapp", "TimeoutParams", uintValue(1)}},
		"undeclared key":      {{"gov", "votingparams", secondsValue(1)}},
		"short value":         {{"gov", "votingparams.voting_period", []byte{1}}},
		"duration overflow":   {{"gov", "votingparams.voting_period", common.BigToHash(new(big.Int).Lsh(big.NewInt(1), 64)).Bytes()}},
		"fails validation":    {{"gov", "votingparams.expedited_voting_period", uintValue(testParams.VotingPeriod)}},
	} {
		_, err := h.submitParamChange(false, changes...)
		require.Error(t, err, name)
	}
	_, err := h.call(alice, callOpts{}, SubmitParamChangeProposalMethod, "", "d", false,
		[]paramChange{{"gov", "votingparams.voting_period", secondsValue(300)}})
	require.Error(t, err, "no title")
	require.Zero(t, newStore(h.state).lastProposalID())
}

func TestParamsReadThroughTheParamsPrecompile(t *testing.T) {
	state := newMemState()
	require.NoError(t, SetParams(state, testParams))
	stored := chainparams.ReadGov(state)
	require.Equal(t, testParams.VotingPeriod, stored.VotingPeriod)
	require.Equal(t, usei(10), stored.MinDeposit, "wei")

	stored.VotingPeriod = 1_500_000_001
	stored.ExpeditedVotingPeriod = 1_000_000_000
	require.NoError(t, chainparams.WriteGov(state, stored))
	require.Equal(t, uint64(1_500_000_001), newStore(state).params().VotingPeriod, "nanoseconds, unrounded")
}

func TestSettleCostCoversEachParamChange(t *testing.T) {
	h := newHarness(t)
	changes := []paramChange{
		{"gov", "votingparams.voting_period", secondsValue(300)},
		{"gov", "votingparams.expedited_voting_period", secondsValue(60)},
		{"gov", "tallyparams.quorum", uintValue(400_000_000_000_000_000)},
	}
	id, err := h.submitParamChange(false, changes...)
	require.NoError(t, err)
	require.GreaterOrEqual(t, must(newStore(h.state).proposalContentChunks(id)), uint64(len(changes)),
		"the content words settleCost charges cover a unit per change")
}

func TestSubmitRejectsOversizedParamChangeContent(t *testing.T) {
	h := newHarness(t)
	changes := make([]paramChange, maxContentLength/(32*8)+1)
	for i := range changes {
		changes[i] = paramChange{"gov", "votingparams.voting_period", secondsValue(300)}
	}
	_, err := h.submitParamChange(false, changes...)
	require.ErrorIs(t, err, errContentTooLong)
}

func TestEveryGovParamChangesThroughAProposal(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	want := Params{
		VotingPeriod:          200 * second,
		ExpeditedVotingPeriod: 30 * second,
		MinDeposit:            usei(20),
		MaxDepositPeriod:      60 * second,
		MinExpeditedDeposit:   usei(50),
		Quorum:                300_000_000_000_000_000,
		Threshold:             550_000_000_000_000_000,
		VetoThreshold:         400_000_000_000_000_000,
		ExpeditedQuorum:       700_000_000_000_000_000,
		ExpeditedThreshold:    750_000_000_000_000_000,
	}
	wantValues := map[string][]byte{
		"votingparams.voting_period":           uintValue(want.VotingPeriod),
		"votingparams.expedited_voting_period": uintValue(want.ExpeditedVotingPeriod),
		"depositparams.min_deposit":            common.BigToHash(want.MinDeposit).Bytes(),
		"depositparams.max_deposit_period":     uintValue(want.MaxDepositPeriod),
		"depositparams.min_expedited_deposit":  common.BigToHash(want.MinExpeditedDeposit).Bytes(),
		"tallyparams.quorum":                   uintValue(want.Quorum),
		"tallyparams.threshold":                uintValue(want.Threshold),
		"tallyparams.veto_threshold":           uintValue(want.VetoThreshold),
		"tallyparams.expedited_quorum":         uintValue(want.ExpeditedQuorum),
		"tallyparams.expedited_threshold":      uintValue(want.ExpeditedThreshold),
	}
	params := func(method string, args ...interface{}) interface{} {
		m := chainparams.ABI.Methods[method]
		input, err := m.Inputs.Pack(args...)
		require.NoError(t, err)
		out, err := chainparams.Precompile{}.Run(&precompiles.Context{Address: chainparams.Address, State: h.state}, append(m.ID, input...))
		require.NoError(t, err, args)
		got, err := m.Outputs.Unpack(out)
		require.NoError(t, err)
		return got[0]
	}
	var keys []chainparams.ParamKey
	abi.ConvertType(params(chainparams.ParamKeysMethod), &keys)
	var changes []paramChange
	for _, k := range keys {
		changes = append(changes, paramChange{k.Subspace, k.Key, wantValues[k.Key]})
	}
	require.Len(t, changes, len(wantValues), "every declared gov param")
	id, err := h.submitParamChange(false, changes...)
	require.NoError(t, err)
	h.passID(id)
	require.Equal(t, StatusPassed, h.status(id))
	require.Equal(t, want, newStore(h.state).params())
	for key, value := range wantValues {
		require.Equal(t, value, params(chainparams.ParamMethod, "gov", key), key)
	}
}

// TestSubspacesFitTheExecutionCharge checks that applying a change, which reads
// its whole subspace one storage word per param, stays within the content words
// settleCost charges for it.
func TestSubspacesFitTheExecutionCharge(t *testing.T) {
	m := chainparams.ABI.Methods[chainparams.ParamKeysMethod]
	out, err := chainparams.Precompile{}.Run(&precompiles.Context{Address: chainparams.Address}, m.ID)
	require.NoError(t, err)
	values, err := m.Outputs.Unpack(out)
	require.NoError(t, err)
	var keys []chainparams.ParamKey
	abi.ConvertType(values[0], &keys)

	perSubspace := map[string]int{}
	shortest := map[string]string{}
	for _, k := range keys {
		typ, err := abi.NewType(k.ValueType, "", nil)
		require.NoError(t, err)
		require.Contains(t, []byte{abi.UintTy, abi.IntTy, abi.BoolTy, abi.AddressTy, abi.FixedBytesTy}, typ.T,
			"%s/%s is stored in one word; a longer kind needs settleCost to charge its words", k.Subspace, k.Key)
		perSubspace[k.Subspace]++
		if s, ok := shortest[k.Subspace]; !ok || len(k.Key) < len(s) {
			shortest[k.Subspace] = k.Key
		}
	}
	for subspace, n := range perSubspace {
		// The smallest valid change: a declared key and a 32-byte value.
		change := chainparams.Change{Subspace: subspace, Key: shortest[subspace], Value: make([]byte, 32)}
		_, one := encodeContent(paramChangeContent{Changes: []chainparams.Change{change}})
		_, two := encodeContent(paramChangeContent{Changes: []chainparams.Change{change, change}})
		perChange := (len(two) - len(one)) / 32
		require.LessOrEqual(t, n, perChange, "subspace %s has more params than a change's %d content words", subspace, perChange)
	}
}
