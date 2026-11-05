package upgrade

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var handlerSlot = common.Hash{0xaa}

func block(n uint64) precompiles.BlockContext {
	return precompiles.BlockContext{Number: n}
}

// markingHandler writes the block number it ran at to handlerSlot.
func markingHandler(block precompiles.BlockContext, state precompiles.State) error {
	state.SetState(Address, handlerSlot, common.BigToHash(new(big.Int).SetUint64(block.Number)))
	return nil
}

func scheduled(t *testing.T, plan Plan) *memState {
	t.Helper()
	state := newMemState()
	require.NoError(t, WriteGenesis(state, []string{"v6.8"}, 1))
	require.NoError(t, Schedule(state, 2, plan))
	return state
}

func requireNoPlan(t *testing.T, state *memState) {
	t.Helper()
	_, ok, err := CurrentPlan(state)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestBeginBlockAppliesADuePlan(t *testing.T) {
	state := scheduled(t, Plan{Name: "v7.0", Height: 10})
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": markingHandler}}

	failures, err := p.BeginBlock(block(10), state)
	require.NoError(t, err)
	require.Empty(t, failures)
	require.Equal(t, common.BigToHash(big.NewInt(10)), state.GetState(Address, handlerSlot))
	require.Equal(t, uint64(10), Applied(state, "v7.0"))
	last, err := LastApplied(state)
	require.NoError(t, err)
	require.Equal(t, "v7.0", last)
	requireNoPlan(t, state)

	failures, err = p.BeginBlock(block(11), state)
	require.NoError(t, err)
	require.Empty(t, failures)
}

func TestBeginBlockRecordsTheBlockALatePlanRanAt(t *testing.T) {
	state := scheduled(t, Plan{Name: "v7.0", Height: 10})
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": nil}}
	_, err := p.BeginBlock(block(15), state)
	require.NoError(t, err)
	require.Equal(t, uint64(15), Applied(state, "v7.0"))
}

func TestBeginBlockHaltsWithoutAHandler(t *testing.T) {
	plan := Plan{Name: "v7.0", Height: 10, Info: `{"upgradeType":"minor"}`}
	state := scheduled(t, plan)
	before := state.clone()
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil}}

	_, err := p.BeginBlock(block(9), state)
	require.NoError(t, err, "a plan that is not due does not halt")
	_, err = p.BeginBlock(block(10), state)
	var needed *NeededError
	require.ErrorAs(t, err, &needed)
	require.Equal(t, plan, needed.Plan)
	require.Equal(t, uint64(10), needed.Block)
	require.Equal(t, `UPGRADE "v7.0" NEEDED at height: 10: {"upgradeType":"minor"}`, err.Error())
	require.Equal(t, before, state)
}

func TestBeginBlockHandlerErrorFailsTheBlock(t *testing.T) {
	state := scheduled(t, Plan{Name: "v7.0", Height: 10})
	refused := errors.New("refused")
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": func(precompiles.BlockContext, precompiles.State) error { return refused }}}
	_, err := p.BeginBlock(block(10), state)
	require.ErrorIs(t, err, refused)
}

func TestBeginBlockSkipsAPlanAtASkipHeight(t *testing.T) {
	state := scheduled(t, Plan{Name: "v7.0", Height: 10})
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil}, SkipHeights: []uint64{10}}
	failures, err := p.BeginBlock(block(10), state)
	require.NoError(t, err)
	require.Len(t, failures, 1)
	require.NotErrorIs(t, failures[0], precompiles.ErrDegraded)
	requireNoPlan(t, state)
	require.Zero(t, Applied(state, "v7.0"), "a skipped plan is not applied")
	require.NoError(t, Schedule(state, 11, Plan{Name: "v7.0", Height: 20}), "a skipped name can be scheduled again")
}

func TestBeginBlockSkipsByTheBlockAPlanFallsDueAt(t *testing.T) {
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil}, SkipHeights: []uint64{10}}
	_, err := p.BeginBlock(block(11), scheduled(t, Plan{Name: "v7.0", Height: 10}))
	var needed *NeededError
	require.ErrorAs(t, err, &needed, "a skip height names the block, not the plan's height")

	p.SkipHeights = []uint64{11}
	state := scheduled(t, Plan{Name: "v7.0", Height: 10})
	_, err = p.BeginBlock(block(11), state)
	require.NoError(t, err)
	requireNoPlan(t, state)
}

func TestBeginBlockHaltsABinaryUpdatedBeforeAMajorPlan(t *testing.T) {
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": nil}}
	_, err := p.BeginBlock(block(9), scheduled(t, Plan{Name: "v7.0", Height: 10}))
	require.ErrorIs(t, err, ErrBinaryUpdatedBeforeTrigger)

	_, err = p.BeginBlock(block(9), scheduled(t, Plan{Name: "v7.0", Height: 10, Info: `{"upgradeType":"minor"}`}))
	require.NoError(t, err, "a minor release's binary may be installed early")
}

func TestBeginBlockHaltsABinaryWithoutTheLastAppliedHandler(t *testing.T) {
	state := newMemState()
	require.NoError(t, WriteGenesis(state, []string{"v6.7", "v6.8"}, 1))
	_, err := Precompile{Handlers: map[string]Handler{"v6.7": nil}}.BeginBlock(block(2), state)
	require.ErrorIs(t, err, ErrWrongBinary)

	_, err = Precompile{}.BeginBlock(block(2), newMemState())
	require.NoError(t, err, "a chain with no applied upgrades runs any binary")
}

func TestBeginBlockReplaySkipsTheBinaryChecks(t *testing.T) {
	replay := block(9)
	replay.Replay = true
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": nil}}
	_, err := p.BeginBlock(replay, scheduled(t, Plan{Name: "v7.0", Height: 10}))
	require.NoError(t, err, "a replay before a plan's height runs on the binary that has its handler")

	state := newMemState()
	require.NoError(t, WriteGenesis(state, []string{"v6.7", "v6.8"}, 1))
	replay = block(2)
	replay.Replay = true
	_, err = Precompile{Handlers: map[string]Handler{"v6.7": nil}}.BeginBlock(replay, state)
	require.NoError(t, err, "a replay does not check the upgrade applied last")
}

func TestBeginBlockReplayAppliesADuePlan(t *testing.T) {
	state := scheduled(t, Plan{Name: "v7.0", Height: 10})
	replay := block(10)
	replay.Replay = true
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": func(_ precompiles.BlockContext, s precompiles.State) error {
		s.SetState(Address, common.Hash{0xaa}, common.Hash{31: 1})
		return nil
	}}}
	_, err := p.BeginBlock(replay, state)
	require.NoError(t, err)
	require.Equal(t, common.Hash{31: 1}, state.GetState(Address, common.Hash{0xaa}), "the replay runs the handler")
	require.Equal(t, uint64(10), Applied(state, "v7.0"))

	_, err = Precompile{Handlers: map[string]Handler{"v6.8": nil}}.BeginBlock(replay, scheduled(t, Plan{Name: "v7.0", Height: 10}))
	var needed *NeededError
	require.ErrorAs(t, err, &needed, "a binary without the handler cannot rebuild the block")
}

func TestBeginBlockRunsOnBlankUpgradeState(t *testing.T) {
	p := Precompile{Handlers: map[string]Handler{"v6.8": nil, "v7.0": nil}}
	// A new chain's first block, and a first block after a handoff at 1,000,000,
	// both run before genesis writes the applied upgrades.
	for _, n := range []uint64{1, 1_000_001} {
		state := newMemState()
		failures, err := p.BeginBlock(block(n), state)
		require.NoError(t, err, n)
		require.Empty(t, failures, n)
		require.Empty(t, state.storage[Address], n)
	}
}

func TestBeginBlockReportsALastAppliedNameThatDoesNotDecode(t *testing.T) {
	state := newMemState()
	state.SetState(Address, solstorage.Slot(slotLastApplied), common.Hash{0: 0xff, 31: 0x01})
	failures, err := Precompile{}.BeginBlock(block(5), state)
	require.NoError(t, err)
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], precompiles.ErrDegraded)

	require.NoError(t, Schedule(state, 5, Plan{Name: "v7.0", Height: 10}))
	p := Precompile{SkipHeights: []uint64{10}}
	failures, err = p.BeginBlock(block(10), state)
	require.NoError(t, err)
	require.Len(t, failures, 2, "the skip reports the check it could not make")
	require.ErrorIs(t, failures[0], precompiles.ErrDegraded)
	requireNoPlan(t, state)
}

func TestBeginBlockClearsAPlanThatDoesNotDecode(t *testing.T) {
	state := newMemState()
	state.SetState(Address, planSlot(planName), common.Hash{0: 0xff, 31: 0x01})
	state.SetState(Address, planSlot(planHeight), common.Hash{31: 5})
	failures, err := Precompile{}.BeginBlock(block(5), state)
	require.NoError(t, err)
	require.Len(t, failures, 1)
	require.ErrorIs(t, failures[0], precompiles.ErrDegraded)
	requireNoPlan(t, state)
}
