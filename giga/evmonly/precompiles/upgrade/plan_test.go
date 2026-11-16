package upgrade

import (
	"math"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestScheduleReplacesAndCancelClears(t *testing.T) {
	state := newMemState()
	require.NoError(t, Schedule(state, 10, Plan{Name: "v7.0", Height: 20, Info: strings.Repeat("i", 40)}))
	require.NoError(t, Schedule(state, 11, Plan{Name: "v7.1", Height: 30}))
	plan, scheduled, err := CurrentPlan(state)
	require.NoError(t, err)
	require.True(t, scheduled)
	require.Equal(t, Plan{Name: "v7.1", Height: 30}, plan)

	require.NoError(t, Cancel(state, "v7.1"))
	_, scheduled, err = CurrentPlan(state)
	require.NoError(t, err)
	require.False(t, scheduled)
	require.Empty(t, state.storage[Address], "cancelling leaves no storage behind")
}

func TestCancelRequiresTheScheduledName(t *testing.T) {
	state := newMemState()
	require.Error(t, Cancel(state, "v7.0"), "no plan is scheduled")

	require.NoError(t, Schedule(state, 10, Plan{Name: "v7.1", Height: 30}))
	before := state.clone()
	require.Error(t, CheckCancel(state, "v7.0"))
	require.Error(t, Cancel(state, "v7.0"), "a replaced plan's name cancels nothing")
	require.Equal(t, before, state, "a refused cancel writes nothing")
	require.NoError(t, CheckCancel(state, "v7.1"))
}

func TestCancelClearsAPlanWhoseInfoDoesNotDecode(t *testing.T) {
	state := newMemState()
	require.NoError(t, Schedule(state, 10, Plan{Name: "v7.0", Height: 30}))
	state.SetState(Address, planSlot(planInfo), common.Hash{0: 0xff, 31: 0x01})
	_, _, err := CurrentPlan(state)
	require.Error(t, err)
	require.NoError(t, Cancel(state, "v7.0"), "a cancel matches only the name")
	requireNoPlan(t, state)
}

func TestScheduleAcceptsTheCurrentBlock(t *testing.T) {
	state := newMemState()
	require.NoError(t, Schedule(state, 20, Plan{Name: "v7.0", Height: 20}))
}

func TestScheduleRejectsAndWritesNothing(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteGenesis(state, []string{"v6.8"}, 1))
	before := state.clone()
	for name, plan := range map[string]Plan{
		"empty name":      {Height: 20},
		"long name":       {Name: strings.Repeat("n", MaxNameLength+1), Height: 20},
		"name with space": {Name: "v7.0 upgrade", Height: 20},
		"name with slash": {Name: "../v7.0", Height: 20},
		"non-ASCII name":  {Name: "v7.0-é", Height: 20},
		"long info":       {Name: "v7.0", Height: 20, Info: strings.Repeat("i", MaxInfoLength+1)},
		"zero height":     {Name: "v7.0"},
		"height too high": {Name: "v7.0", Height: math.MaxInt64 + 1},
		"past height":     {Name: "v7.0", Height: 9},
		"applied name":    {Name: "v6.8", Height: 20},
	} {
		require.Error(t, Schedule(state, 10, plan), name)
		require.Equal(t, before, state, name)
	}
}

func TestScheduleOverwritesAPlanThatDoesNotDecode(t *testing.T) {
	state := newMemState()
	state.SetState(Address, planSlot(planName), common.Hash{0: 0xff, 31: 0x01})
	state.SetState(Address, planSlot(planHeight), common.Hash{31: 5})
	_, _, err := CurrentPlan(state)
	require.Error(t, err)

	require.NoError(t, Schedule(state, 1, Plan{Name: "v7.0", Height: 20}))
	plan, _, err := CurrentPlan(state)
	require.NoError(t, err)
	require.Equal(t, Plan{Name: "v7.0", Height: 20}, plan)
}

func TestWriteGenesisRecordsEveryName(t *testing.T) {
	state := newMemState()
	require.False(t, HasGenesis(state))
	require.NoError(t, WriteGenesis(state, []string{"1.0.2beta", "v6.7", "v6.8"}, 3))
	require.True(t, HasGenesis(state))
	for _, name := range []string{"1.0.2beta", "v6.7", "v6.8"} {
		require.Equal(t, uint64(3), Applied(state, name), name)
	}
	require.Zero(t, Applied(state, "v6.9"))
	last, err := LastApplied(state)
	require.NoError(t, err)
	require.Equal(t, "v6.8", last)
	require.Error(t, WriteGenesis(state, []string{"v6.9"}, 4), "genesis is written once")
}

func TestValidateNameAllowsSemverCharacters(t *testing.T) {
	for _, name := range []string{"v7.0", "v7.0.1-rc.1+build.5", "vX.Y-test", "7"} {
		require.NoError(t, ValidateName(name), name)
	}
}

func TestWriteGenesisAcceptsNamesOutsideSemverCharacters(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteGenesis(state, []string{"1.0.5beta upgrade", "v6.8"}, 1))
	require.Equal(t, uint64(1), Applied(state, "1.0.5beta upgrade"))
}

func TestWriteGenesisRejectsAndWritesNothing(t *testing.T) {
	for name, names := range map[string][]string{
		"empty":    {"v6.7", ""},
		"long":     {strings.Repeat("n", MaxNameLength+1)},
		"repeated": {"v6.7", "v6.8", "v6.7"},
	} {
		state := newMemState()
		require.Error(t, WriteGenesis(state, names, 1), name)
		require.Empty(t, state.storage, name)
	}
	state := newMemState()
	require.NoError(t, WriteGenesis(state, nil, 1))
	require.Empty(t, state.storage)
}

func TestIsMinorRelease(t *testing.T) {
	for info, minor := range map[string]bool{
		"":                              false,
		`{"upgradeType":"minor"}`:       true,
		`{"upgradeType":"MINOR"}`:       true,
		`{"upgradeType":"major"}`:       false,
		`{"binaries":{}}`:               false,
		`not json`:                      false,
		`{"upgradeType":"minor"`:        false,
		`{"upgradeType":"minor","x":1}`: true,
	} {
		require.Equal(t, minor, Plan{Info: info}.IsMinorRelease(), info)
	}
}
