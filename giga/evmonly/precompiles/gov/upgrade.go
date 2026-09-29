package gov

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Upgrades is the part a binary plays in scheduled software upgrades.
type Upgrades struct {
	// Name is the upgrade name this binary applies, its source commit. Empty applies none.
	Name string
	// SkipHeights are heights at which a due plan is skipped rather than stopping the node.
	SkipHeights []uint64
}

// ErrUpgradeBeforeTrigger reports a binary started before the height of the upgrade it applies.
var ErrUpgradeBeforeTrigger = errors.New("binary updated before its upgrade height")

// UpgradeNeededError fails a block at the height of a scheduled plan the binary does not apply.
type UpgradeNeededError struct {
	Plan Plan
}

func (e *UpgradeNeededError) Error() string {
	return fmt.Sprintf("UPGRADE \"%s\" NEEDED at height: %d: %s", e.Plan.Name, e.Plan.Height, e.Plan.Info)
}

// CheckStart returns ErrUpgradeBeforeTrigger when db schedules the upgrade u
// applies at a height after next, the first block this binary would execute.
func (u Upgrades) CheckStart(db Reader, next uint64) error {
	plan, ok := ReadPlan(db)
	if !ok || u.Name == "" || plan.Name != u.Name || plan.Height <= next {
		return nil
	}
	return fmt.Errorf("%w: upgrade %q is scheduled at height %d and the next block is %d",
		ErrUpgradeBeforeTrigger, plan.Name, plan.Height, next)
}

// BeginBlock fails a block at or past the height of a scheduled plan this
// binary does not apply with an *UpgradeNeededError, unless the block's height
// is skipped.
func (c *Contract) BeginBlock(block precompiles.BlockContext, state precompiles.StateReader) error {
	plan, ok := store{addr: c.addr, db: state}.plan()
	if !ok || plan.Height > block.Number || plan.Name == c.upgrades.Name || slices.Contains(c.upgrades.SkipHeights, block.Number) {
		return nil
	}
	return &UpgradeNeededError{Plan: plan}
}
