package gov

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
)

// upgradePlan is the ABI tuple submitSoftwareUpgradeProposal takes.
type upgradePlan struct {
	Name   string
	Height uint64
	Info   string
}

func (h *harness) submitUpgrade(plan upgradePlan) (uint64, error) {
	values, err := h.call(alice, callOpts{value: usei(10)}, SubmitSoftwareUpgradeMethod, "t", "d", false, plan)
	if err != nil {
		return 0, err
	}
	return values[0].(uint64), nil
}

func (h *harness) currentPlan() (upgrade.Plan, bool) {
	plan, ok, err := upgrade.CurrentPlan(h.state)
	require.NoError(h.t, err)
	return plan, ok
}

func TestSoftwareUpgradeSchedulesWhenPassed(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: 100, Info: `{"upgradeType":"minor"}`})
	require.NoError(t, err)
	require.Equal(t, contentSoftwareUpgrade, h.proposal(id).ContentType)
	_, ok := h.currentPlan()
	require.False(t, ok, "nothing is scheduled before the proposal passes")

	h.passID(id)
	require.Equal(t, StatusPassed, h.status(id))
	plan, ok := h.currentPlan()
	require.True(t, ok)
	require.Equal(t, upgrade.Plan{Name: "v7.0", Height: 100, Info: `{"upgradeType":"minor"}`}, plan)
}

func TestSoftwareUpgradeReplacesTheScheduledPlan(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	first, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: 100})
	require.NoError(t, err)
	h.passID(first)
	second, err := h.submitUpgrade(upgradePlan{Name: "v7.1", Height: 200})
	require.NoError(t, err)
	h.passID(second)
	plan, _ := h.currentPlan()
	require.Equal(t, upgrade.Plan{Name: "v7.1", Height: 200}, plan)
}

func TestSoftwareUpgradeAtTheExecutingBlockIsScheduled(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: h.number})
	require.NoError(t, err)
	h.passID(id)
	require.Equal(t, StatusPassed, h.status(id))
	plan, ok := h.currentPlan()
	require.True(t, ok)
	require.Equal(t, h.number-1, plan.Height, "the plan falls due at the next block's BeginBlock")
}

func TestSoftwareUpgradeThatFellBehindFails(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: h.number})
	require.NoError(t, err)
	h.number++
	h.passID(id)
	require.Equal(t, StatusFailed, h.status(id))
	_, ok := h.currentPlan()
	require.False(t, ok)
}

func TestSoftwareUpgradeOfAnAppliedNameFails(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitUpgrade(upgradePlan{Name: "v6.8", Height: 100})
	require.NoError(t, err)
	require.NoError(t, upgrade.WriteGenesis(h.state, []string{"v6.8"}, 1))

	_, err = h.submitUpgrade(upgradePlan{Name: "v6.8", Height: 100})
	require.Error(t, err)
	require.NotErrorIs(t, err, vm.ErrExecutionReverted, "a check against state fails without revert data")

	h.passID(id)
	require.Equal(t, StatusFailed, h.status(id), "the name was applied while the proposal was voting")
}

func TestSoftwareUpgradeRejectsAnInvalidPlan(t *testing.T) {
	h := newHarness(t)
	for name, plan := range map[string]upgradePlan{
		"empty name":      {Height: 100},
		"name with space": {Name: "v7.0 upgrade", Height: 100},
		"zero height":     {Name: "v7.0"},
	} {
		_, err := h.submitUpgrade(plan)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
	}
	h.number = 10
	_, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: 9})
	require.Error(t, err, "a height below the current block")
	require.NotErrorIs(t, err, vm.ErrExecutionReverted)
}

func (h *harness) submitCancel(name string) (uint64, error) {
	values, err := h.call(alice, callOpts{value: usei(10)}, SubmitCancelUpgradeMethod, "t", "d", false, name)
	if err != nil {
		return 0, err
	}
	return values[0].(uint64), nil
}

func TestCancelSoftwareUpgradeClearsThePlan(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: 100})
	require.NoError(t, err)
	h.passID(id)

	cancel, err := h.submitCancel("v7.0")
	require.NoError(t, err)
	require.Equal(t, contentCancelSoftwareUpgrade, h.proposal(cancel).ContentType)
	h.passID(cancel)
	require.Equal(t, StatusPassed, h.status(cancel))
	_, ok := h.currentPlan()
	require.False(t, ok)
}

func TestCancelSoftwareUpgradeRejectsABadName(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	for name, args := range map[string][]interface{}{
		"empty title": {"", "d", false, "v7.0"},
		"empty name":  {"t", "d", false, ""},
		"long name":   {"t", "d", false, strings.Repeat("n", upgrade.MaxNameLength+1)},
		"slash name":  {"t", "d", false, "../v7.0"},
	} {
		_, err := h.call(alice, callOpts{}, SubmitCancelUpgradeMethod, args...)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
	}

	_, err := h.submitCancel("v7.0")
	require.Error(t, err, "no plan is scheduled")
	require.NotErrorIs(t, err, vm.ErrExecutionReverted, "a check against state fails without revert data")

	id, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: 100})
	require.NoError(t, err)
	h.passID(id)
	_, err = h.submitCancel("v7.1")
	require.Error(t, err, "the scheduled plan has another name")
	require.NotErrorIs(t, err, vm.ErrExecutionReverted)
}

func TestCancelSoftwareUpgradeOfAReplacedPlanFails(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id, err := h.submitUpgrade(upgradePlan{Name: "v7.0", Height: 100})
	require.NoError(t, err)
	h.passID(id)
	cancel, err := h.submitCancel("v7.0")
	require.NoError(t, err)
	values, err := h.call(alice, callOpts{value: usei(40)}, SubmitSoftwareUpgradeMethod, "t", "d", true, upgradePlan{Name: "v7.1", Height: 200})
	require.NoError(t, err)
	replace := values[0].(uint64)
	for _, voter := range []common.Address{alice, bob, carol} {
		h.vote(voter, cancel, OptionYes)
		h.vote(voter, replace, OptionYes)
	}

	start := h.time
	h.time = start + testParams.ExpeditedVotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusPassed, h.status(replace), "the expedited replacement passes while the cancel votes")
	h.time = start + testParams.VotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusFailed, h.status(cancel), "the plan it named was replaced while it was voting")
	plan, ok := h.currentPlan()
	require.True(t, ok)
	require.Equal(t, upgrade.Plan{Name: "v7.1", Height: 200}, plan)
}
