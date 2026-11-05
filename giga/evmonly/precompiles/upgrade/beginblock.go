package upgrade

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Handler applies a named upgrade at the start of the block it runs in. Its
// writes land in that block's changeset; an error fails the block.
type Handler func(precompiles.BlockContext, precompiles.State) error

// NeededError halts the chain at Block, where Plan is due and this binary
// has no handler for it.
type NeededError struct {
	Plan  Plan
	Block uint64
}

func (e *NeededError) Error() string {
	return fmt.Sprintf("UPGRADE \"%s\" NEEDED at height: %d: %s", e.Plan.Name, e.Plan.Height, e.Plan.Info)
}

// ErrBinaryUpdatedBeforeTrigger halts a binary that has the handler for a
// major release plan that is not due yet.
var ErrBinaryUpdatedBeforeTrigger = errors.New("BINARY UPDATED BEFORE TRIGGER")

// ErrWrongBinary halts a binary without the handler for the upgrade applied last.
var ErrWrongBinary = errors.New("wrong binary")

// BeginBlock applies, skips or halts on a due plan, and halts a binary that
// does not match the chain's upgrade history unless block.Replay is set.
func (p Precompile) BeginBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	s := newStore(state)
	plan, scheduled, err := CurrentPlan(state)
	if err != nil {
		if clearErr := writePlan(s, Plan{}); clearErr != nil {
			return nil, clearErr
		}
		return []error{fmt.Errorf("%w: cleared an upgrade plan that does not decode: %w", precompiles.ErrDegraded, err)}, nil
	}
	if scheduled && plan.Height <= block.Number {
		return p.applyDue(block, state, plan)
	}
	failures, err := p.checkLastApplied(block, state)
	if err != nil {
		return nil, err
	}
	if scheduled && !block.Replay && !plan.IsMinorRelease() && p.hasHandler(plan.Name) {
		return nil, fmt.Errorf("%w! UPGRADE \"%s\" - in binary but not executed on chain", ErrBinaryUpdatedBeforeTrigger, plan.Name)
	}
	return failures, nil
}

// applyDue runs the due plan's handler and records it applied at this block,
// clears the plan when this is a block the node skips, or halts.
func (p Precompile) applyDue(block precompiles.BlockContext, state precompiles.State, plan Plan) ([]error, error) {
	if slices.Contains(p.SkipHeights, block.Number) {
		failures, err := p.checkLastApplied(block, state)
		if err != nil {
			return nil, err
		}
		if err := writePlan(newStore(state), Plan{}); err != nil {
			return nil, err
		}
		return append(failures, fmt.Errorf("UPGRADE \"%s\" SKIPPED at %d: %s", plan.Name, plan.Height, plan.Info)), nil
	}
	handler, ok := p.Handlers[plan.Name]
	if !ok {
		return nil, &NeededError{Plan: plan, Block: block.Number}
	}
	if handler != nil {
		if err := handler(block, state); err != nil {
			return nil, fmt.Errorf("upgrade %q handler: %w", plan.Name, err)
		}
	}
	return nil, markApplied(newStore(state), plan.Name, block.Number)
}

// checkLastApplied returns ErrWrongBinary when this binary has no handler for
// the upgrade applied last, and a degraded failure when that upgrade's name
// does not decode. It checks nothing when block.Replay is set.
func (p Precompile) checkLastApplied(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if block.Replay {
		return nil, nil
	}
	last, err := LastApplied(state)
	if err != nil {
		return []error{fmt.Errorf("%w: skipped the wrong-binary check, the upgrade applied last does not decode: %w", precompiles.ErrDegraded, err)}, nil
	}
	if last != "" && !p.hasHandler(last) {
		return nil, fmt.Errorf("%w: upgrade handler is missing for %q, the upgrade applied last", ErrWrongBinary, last)
	}
	return nil, nil
}

func (p Precompile) hasHandler(name string) bool {
	_, ok := p.Handlers[name]
	return ok
}
