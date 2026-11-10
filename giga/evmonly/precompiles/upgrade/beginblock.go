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

// NeededError halts the chain at a due plan this binary has no handler for.
type NeededError struct {
	Plan Plan
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
	plan, scheduled, err := scheduledPlan(state)
	if err != nil {
		return clearUndecodablePlan(state, err)
	}
	if scheduled && plan.Height <= block.Number {
		if plan.Info, err = planInfoOf(state); err != nil {
			return clearUndecodablePlan(state, err)
		}
		return p.applyDue(block, state, plan)
	}
	failures, err := p.checkLastApplied(block, state)
	if err != nil {
		return nil, err
	}
	if scheduled && !block.Replay && p.hasHandler(plan.Name) {
		return p.checkEarlyBinary(state, plan, failures)
	}
	return failures, nil
}

// clearUndecodablePlan clears the scheduled plan and reports it as degraded.
func clearUndecodablePlan(state precompiles.State, err error) ([]error, error) {
	if clearErr := writePlan(newStore(state), Plan{}); clearErr != nil {
		return nil, clearErr
	}
	return []error{fmt.Errorf("%w: cleared an upgrade plan that does not decode: %w", precompiles.ErrDegraded, err)}, nil
}

// checkEarlyBinary returns ErrBinaryUpdatedBeforeTrigger unless plan, not yet
// due, is a minor release, and a degraded failure when its Info does not decode.
func (p Precompile) checkEarlyBinary(state precompiles.State, plan Plan, failures []error) ([]error, error) {
	info, err := planInfoOf(state)
	if err != nil {
		// Only binaries holding the plan's handler get here, so clearing the
		// plan would split the chain's state; the plan clears once it is due.
		return append(failures, fmt.Errorf("%w: skipped the early-binary check, the scheduled plan does not decode: %w", precompiles.ErrDegraded, err)), nil
	}
	plan.Info = info
	if !plan.IsMinorRelease() {
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
		return nil, &NeededError{Plan: plan}
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
