package gov_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
)

var (
	oldBinary    = strings.Repeat("0a", 20)
	targetBinary = strings.Repeat("1b", 20)
)

// passUpgrade schedules an upgrade to name at height through a single-voter
// proposal, which passes two blocks later.
func (c *chain) passUpgrade(f fixture, id uint64, name string, height int64) {
	c.t.Helper()
	requireStatuses(c.t, c.block(1, f.submit(f.voters[0], upgradeJSON(name, height)), f.vote(f.voters[0], id, gov.OptionYes)), 1, 1)
	c.block(votingPeriod)
	var p proposalData
	c.query(&p, "proposal", id)
	require.Equal(c.t, gov.StatusPassed, p.Status)
}

// advanceTo executes empty blocks until number is the last one executed.
func (c *chain) advanceTo(number uint64) {
	c.t.Helper()
	for c.number < number {
		c.block(1)
	}
}

func (c *chain) doneHeight(name string) (uint64, bool) {
	view := c.store.OpenView()
	defer view.Close()
	return gov.ReadDoneHeight(viewReader{view}, name)
}

func requireUpgradeNeeded(t testing.TB, err error, want gov.Plan) {
	t.Helper()
	needed, ok := errors.AsType[*gov.UpgradeNeededError](err)
	require.True(t, ok, "want an UpgradeNeededError, got %v", err)
	require.Equal(t, want, needed.Plan)
}

func TestUpgradeNeededMessageMatchesXUpgrade(t *testing.T) {
	err := &gov.UpgradeNeededError{Plan: gov.Plan{Name: targetBinary, Height: 42, Info: `{"binaries":{}}`, Proposal: 7}}
	require.Equal(t, `UPGRADE "`+targetBinary+`" NEEDED at height: 42: {"binaries":{}}`, err.Error())
}

func TestOldBinaryStopsAtThePlanHeightAndTheTargetBinaryContinues(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, workers int) {
		f := newFixture(t, 1)
		f.upgrades = gov.Upgrades{Name: oldBinary}
		c := f.newChain(t, workers)
		c.passUpgrade(f, 1, targetBinary, 10)
		want := gov.Plan{Name: targetBinary, Height: 10, Info: "info for " + targetBinary, Proposal: 1}

		// Every block below the plan height runs.
		c.advanceTo(9)
		plan, ok := c.plan()
		require.True(t, ok)
		require.Equal(t, want, plan)

		// The old binary refuses block 10, with or without transactions, and on every retry.
		_, err := c.tryBlock(1, f.submit(f.voters[0], upgradeJSON("later", 100)))
		requireUpgradeNeeded(t, err, want)
		c.swapBinary(f, gov.Upgrades{Name: oldBinary})
		_, err = c.tryBlock(1)
		requireUpgradeNeeded(t, err, want)
		require.Equal(t, uint64(9), c.number)
		plan, ok = c.plan()
		require.True(t, ok)
		require.Equal(t, want, plan)
		_, ok = c.doneHeight(targetBinary)
		require.False(t, ok)

		// The target binary runs block 10, which completes the plan.
		c.swapBinary(f, gov.Upgrades{Name: targetBinary})
		requireStatuses(t, c.block(1, f.submit(f.voters[0], upgradeJSON("later", 100))), 1)
		_, ok = c.plan()
		require.False(t, ok)
		done, ok := c.doneHeight(targetBinary)
		require.True(t, ok)
		require.Equal(t, uint64(10), done)
		c.advanceTo(12)
	})
}

func TestBinaryWithoutANameStopsAtThePlanHeight(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	c.passUpgrade(f, 1, targetBinary, 3)
	_, err := c.tryBlock(1)
	requireUpgradeNeeded(t, err, gov.Plan{Name: targetBinary, Height: 3, Info: "info for " + targetBinary, Proposal: 1})
}

func TestPlanPassedInTheBlockBeforeItsHeightStopsTheNextBlock(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, workers int) {
		f := newFixture(t, 1)
		f.upgrades = gov.Upgrades{Name: oldBinary}
		c := f.newChain(t, workers)
		// The proposal passes at the end of block 2, and the plan is due at block 3.
		c.passUpgrade(f, 1, targetBinary, 3)
		_, err := c.tryBlock(1)
		requireUpgradeNeeded(t, err, gov.Plan{Name: targetBinary, Height: 3, Info: "info for " + targetBinary, Proposal: 1})
	})
}

func TestSkipHeightCompletesThePlanLikeTheTargetBinary(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, workers int) {
		f := newFixture(t, 1)
		changes := map[string]evmonly.StateChangeSet{}
		for name, upgrades := range map[string]gov.Upgrades{
			"target": {Name: targetBinary},
			"skip":   {Name: oldBinary, SkipHeights: []uint64{4, 10, 20}},
		} {
			f.upgrades = gov.Upgrades{Name: oldBinary}
			c := f.newChain(t, workers)
			c.passUpgrade(f, 1, targetBinary, 10)
			c.advanceTo(9)
			c.swapBinary(f, upgrades)
			changes[name] = c.block(1).ChangeSet
			_, ok := c.plan()
			require.False(t, ok)
			done, ok := c.doneHeight(targetBinary)
			require.True(t, ok)
			require.Equal(t, uint64(10), done)
		}
		require.Equal(t, changes["target"], changes["skip"])
	})
}

func TestSkipHeightOtherThanThePlanHeightStillStops(t *testing.T) {
	f := newFixture(t, 1)
	f.upgrades = gov.Upgrades{Name: oldBinary, SkipHeights: []uint64{9, 11}}
	c := f.newChain(t, 1)
	c.passUpgrade(f, 1, targetBinary, 10)
	c.advanceTo(9)
	_, err := c.tryBlock(1)
	requireUpgradeNeeded(t, err, gov.Plan{Name: targetBinary, Height: 10, Info: "info for " + targetBinary, Proposal: 1})
}

func TestCancelledPlanDoesNotStopTheOldBinary(t *testing.T) {
	f := newFixture(t, 1)
	f.upgrades = gov.Upgrades{Name: oldBinary}
	c := f.newChain(t, 1)
	c.passUpgrade(f, 1, targetBinary, 10)
	requireStatuses(t, c.block(1, f.submit(f.voters[0], cancelJSON()), f.vote(f.voters[0], 2, gov.OptionYes)), 1, 1)
	c.block(votingPeriod)
	_, ok := c.plan()
	require.False(t, ok)
	c.advanceTo(12)
	_, ok = c.doneHeight(targetBinary)
	require.False(t, ok)
}

func TestReplacedPlanStopsOnlyAtItsOwnHeight(t *testing.T) {
	f := newFixture(t, 1)
	f.upgrades = gov.Upgrades{Name: oldBinary}
	c := f.newChain(t, 1)
	c.passUpgrade(f, 1, targetBinary, 10)
	later := strings.Repeat("2c", 20)
	c.passUpgrade(f, 2, later, 12)
	c.advanceTo(11)
	_, ok := c.doneHeight(targetBinary)
	require.False(t, ok)
	_, err := c.tryBlock(1)
	requireUpgradeNeeded(t, err, gov.Plan{Name: later, Height: 12, Info: "info for " + later, Proposal: 2})
}

func TestCompletedUpgradeCannotBeScheduledAgain(t *testing.T) {
	f := newFixture(t, 1)
	f.upgrades = gov.Upgrades{Name: targetBinary}
	c := f.newChain(t, 1)
	c.passUpgrade(f, 1, targetBinary, 5)
	c.advanceTo(5)
	_, ok := c.plan()
	require.False(t, ok)

	requireStatuses(t, c.block(1, f.submit(f.voters[0], upgradeJSON(targetBinary, 100)), f.vote(f.voters[0], 2, gov.OptionYes)), 1, 1)
	c.block(votingPeriod)
	var p proposalData
	c.query(&p, "proposal", uint64(2))
	require.Equal(t, gov.StatusFailed, p.Status)
	_, ok = c.plan()
	require.False(t, ok)
	done, ok := c.doneHeight(targetBinary)
	require.True(t, ok)
	require.Equal(t, uint64(5), done)
}

func TestCheckStart(t *testing.T) {
	f := newFixture(t, 1)
	f.upgrades = gov.Upgrades{Name: oldBinary}
	c := f.newChain(t, 1)
	check := func(upgrades gov.Upgrades, next uint64) error {
		view := c.store.OpenView()
		defer view.Close()
		return upgrades.CheckStart(viewReader{view}, next)
	}
	require.NoError(t, check(gov.Upgrades{Name: targetBinary}, 1), "no plan")

	c.passUpgrade(f, 1, targetBinary, 10)
	for _, next := range []uint64{3, 9} {
		require.ErrorIs(t, check(gov.Upgrades{Name: targetBinary}, next), gov.ErrUpgradeBeforeTrigger)
		require.ErrorIs(t, check(gov.Upgrades{Name: targetBinary, SkipHeights: []uint64{10}}, next), gov.ErrUpgradeBeforeTrigger)
	}
	for _, upgrades := range []gov.Upgrades{{Name: oldBinary}, {}} {
		require.NoError(t, check(upgrades, 3))
	}
	require.NoError(t, check(gov.Upgrades{Name: targetBinary}, 10))
	require.NoError(t, check(gov.Upgrades{Name: targetBinary}, 11))
}

// TestUpgradeDoesNotTouchOtherStorage requires completing a plan to write only
// the governance precompile's own storage.
func TestUpgradeDoesNotTouchOtherStorage(t *testing.T) {
	f := newFixture(t, 1)
	f.upgrades = gov.Upgrades{Name: targetBinary}
	c := f.newChain(t, 1)
	c.passUpgrade(f, 1, targetBinary, 5)
	c.advanceTo(4)
	changes := c.block(1).ChangeSet
	require.Empty(t, changes.Balances)
	require.Empty(t, changes.Code)
	require.Empty(t, changes.StorageClears)
	require.NotEmpty(t, changes.Storage)
	for _, change := range changes.Storage {
		require.Equal(t, gov.Address, common.Address(change.Address))
	}
}
