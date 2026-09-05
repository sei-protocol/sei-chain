// Persistence for consensus state.
//
// # What We Persist
//
// All consensus state is persisted atomically in a single A/B file pair (inner_a.pb/inner_b.pb):
//   - Index: current view RoadIndex for ErrAvailBehindConsensus on restore
//   - TimeoutQC: justified entering the current view number
//   - PrepareQC: needed for timeoutVote on restart
//   - PrepareVote, CommitVote, TimeoutVote: this node's votes for the current view
//
// Runtime CommitQC + next-view epoch come from avail's ConsensusSpec, not the WAL.
//
// # Why We Persist
//
// Safety: Votes prevent double-voting on restart - a critical safety property.
//
// Liveness: View justification (QCs) enables fast view synchronization after
// cluster-wide outages. Without persisted QCs, lagging validators would be stuck.
//
// Example failure case without QC persistence:
//   - All validators have CommitQC for index 4
//   - Within index 5, validators timeout multiple times (view numbers 0→1→2→3)
//   - A, B, C reach view (5, 3) via TimeoutQC for view (5, 2)
//   - D, E are slower, only at view (5, 2) via TimeoutQC for view (5, 1)
//   - Cluster crashes
//   - Without persisted QCs, nodes only have their timeout VOTES
//   - A, B, C have timeout votes for (5, 2), D, E have timeout votes for (5, 1)
//   - On restart, D, E need TimeoutQC(5, 1) to justify being at (5, 2)
//   - But TimeoutQC(5, 1) requires 2/3 votes for view (5, 1)
//   - Only D, E have those votes - not enough for quorum
//   - D, E are stuck at view (5, 1), cannot advance
//
// With QC persistence:
//   - A, B, C have TimeoutQC(5, 2) persisted - justifies view (5, 3)
//   - D, E have TimeoutQC(5, 1) persisted - justifies view (5, 2)
//   - On restart, everyone rebroadcasts their persisted QCs
//   - D, E are already at (5, 2), they broadcast TimeoutQC(5, 1) (helps no one)
//   - A, B, C broadcast TimeoutQC(5, 2)
//   - D, E receive TimeoutQC(5, 2), can now advance to (5, 3)
//   - Everyone converges to the highest view
//
// # PersistentStateDir Configuration
//
// At config level, PersistentStateDir is an Option[string]. NewState creates a persister when
// it is Some(path). When None, no persister is created (persistence
// disabled - DANGEROUS, may lead to SLASHING if the node restarts and double-votes;
// only use for testing). When Some(path), the path must already exist and be
// writable (verified by writing a temp file at startup); returns error otherwise.
// TODO: surface the None warning in CLI --help (e.g. stream command or config docs).
//
// # Recovery Behavior
//
//   - Fresh start (files don't exist): Logged at INFO level, node starts clean
//   - Successful restore: Logged at INFO level with state details
//   - One file corrupt (e.g. crash during write): Uses the other file; logged at WARN
//   - Both files corrupt or unreadable: Returns error to caller
//   - Inconsistent state: Returns error to caller with message indicating which field is corrupt
//     Examples of inconsistent state:
//   - Vote from a future view (how could we vote for a view we haven't reached?)
//   - TimeoutQC.View().Index != Index
//   - WAL Index ahead of ConsensusSpec: ErrAvailBehindConsensus
//   - Spec ahead of WAL Index: advance to spec, discard per-view WAL state
//   - Equal Index: keep WAL votes/QCs if persistedInner.Verify(spec, self) passes
//     (e.g. reject future-view votes, TimeoutQC index ≠ spec.Index(),
//     bad signatures, CommitVote without PrepareQC)
//
// # Write Behavior
//
//   - State directory must already exist (we do not create it).
//   - Writes are synchronous.
//   - Writes are idempotent, so retries on next state change are safe
//
// # Rebroadcasting
//
// On restart, the consensus layer propagates loaded state to output watches,
// which triggers rebroadcasting to peers:
//   - Votes (prepareVote, commitVote, timeoutVote): YES - rebroadcast via sendUpdates
//   - TimeoutQC: YES - rebroadcast via myTimeoutQC watch
//   - CommitQC: NO - used locally for view justification but not rebroadcast;
//     the runtime tip comes from ConsensusSpec, while the WAL stores only
//     Index. CommitQCs are served via StreamCommitQCs from the data
//     layer. TODO: consider rebroadcasting CommitQC on restart to help peers
//     sync faster after cluster-wide outages.
package consensus

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "consensus")

// Persisted state file prefix for consensus inner state.
const innerFile = "inner"

type inner struct {
	persistedInner
	spec types.ConsensusSpec
}

// View returns the current view, embedding the epoch's index.
func (i inner) View() types.View {
	vs := types.ViewSpec{ConsensusSpec: i.spec, TimeoutQC: i.TimeoutQC}
	return vs.View()
}

// newInner restores consensus state from avail's ConsensusSpec. The tip CommitQC
// and next-view epoch always come from the spec. The WAL is kept only for
// same-view votes / TimeoutQC / PrepareQC when Index matches spec.Index(); local
// votes must be signed by self. Returns ErrAvailBehindConsensus when WAL Index
// is ahead of the spec.
func newInner(
	loaded utils.Option[*pb.PersistedInner],
	spec types.ConsensusSpec,
	self types.PublicKey,
) (inner, error) {
	var persisted persistedInner
	persistedViewIdx := types.RoadIndex(0)
	if p, ok := loaded.Get(); ok {
		decoded, err := innerProtoConv.Decode(p)
		if err != nil {
			return inner{}, fmt.Errorf("corrupt persisted state: %w", err)
		}
		persisted = *decoded
		persistedViewIdx = persisted.Index
	}

	specViewIdx := spec.Index()
	if persistedViewIdx > specViewIdx {
		return inner{}, fmt.Errorf("%w: persisted view %d > ConsensusSpec view %d",
			ErrAvailBehindConsensus, persistedViewIdx, specViewIdx)
	}

	if specViewIdx == persistedViewIdx {
		// Same view: keep WAL votes / view QCs; CommitQC+epoch from the spec.
		if err := persisted.Verify(spec, self); err != nil {
			return inner{}, err
		}
		logger.Info("restored consensus state", "state", innerProtoConv.Encode(&persisted))
		return inner{persistedInner: persisted, spec: spec}, nil
	}

	out := persistedInner{Index: spec.Index()}
	logger.Info("restored consensus state from avail ConsensusSpec", "state", innerProtoConv.Encode(&out))
	return inner{persistedInner: out, spec: spec}, nil
}

// pushSpec advances consensus to avail's ConsensusSpec tip and clears per-view
// state. Specs that do not advance the view are ignored, which covers the
// tipless spec published before the first CommitQC.
func (s *State) pushSpec(spec types.ConsensusSpec) error {
	specViewIdx := spec.Index()
	if specViewIdx <= s.innerRecv.Load().spec.Index() {
		return nil
	}
	for iSend := range s.inner.Lock() {
		i := iSend.Load()
		if specViewIdx <= i.spec.Index() {
			return nil
		}
		// CommitQC advances to new index; clear all state for new view.
		iSend.Store(inner{
			persistedInner: persistedInner{Index: spec.Index()},
			spec:           spec,
		})
	}
	return nil
}

func (s *State) waitForView(ctx context.Context, view types.View) (types.ViewSpec, error) {
	return s.myView.Wait(ctx, func(v types.ViewSpec) bool { return !v.View().Less(view) })
}

func (s *State) pushTimeoutQC(ctx context.Context, qc *types.TimeoutQC) error {
	i, err := s.innerRecv.Wait(ctx, func(i inner) bool { return i.View().Index >= qc.View().Index })
	if err != nil {
		return err
	}
	if qc.View().Less(i.View()) {
		return nil
	}
	// Verify checks the invariant: TimeoutQC.View().Index == CommitQC.Index + 1
	if err := qc.Verify(i.spec.Epoch, i.spec.CommitQC); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if qc.View().Less(i.View()) {
			return nil
		}
		// TimeoutQC advances view number; clear votes and prepareQC (stale view).
		isend.Store(inner{
			persistedInner: persistedInner{Index: i.Index, TimeoutQC: utils.Some(qc)},
			spec:           i.spec,
		})
	}
	return nil
}

// pushProposal processes an unverified FullProposal message.
func (s *State) pushProposal(ctx context.Context, proposal *types.FullProposal) error {
	// Wait for view.
	vs, err := s.waitForView(ctx, proposal.View())
	if err != nil {
		return err
	}
	// Verify message.
	if vs.View() != proposal.View() {
		return nil
	}
	if err := proposal.Verify(vs); err != nil {
		return fmt.Errorf("proposal.Verify(): %w", err)
	}
	// Update.
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if i.View() != proposal.View() || i.TimeoutVote.IsPresent() || i.PrepareVote.IsPresent() {
			return nil
		}
		v := types.Sign(s.cfg.Key, types.NewPrepareVote(proposal.Proposal().Msg()))
		i.PrepareVote = utils.Some(v)
		isend.Store(i)
	}
	return nil
}

func (s *State) pushPrepareQC(ctx context.Context, qc *types.PrepareQC) error {
	// Wait for view.
	vs, err := s.waitForView(ctx, qc.Proposal().View())
	if err != nil {
		return err
	}
	// Verify message.
	if vs.View() != qc.Proposal().View() {
		return nil
	}
	if err := qc.Verify(vs.Epoch); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	// Update.
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if i.View() != qc.Proposal().View() || i.TimeoutVote.IsPresent() || i.PrepareQC.IsPresent() {
			return nil
		}
		i.PrepareQC = utils.Some(qc)
		v := types.Sign(s.cfg.Key, types.NewCommitVote(qc.Proposal()))
		i.CommitVote = utils.Some(v)
		isend.Store(i)
	}
	return nil
}

func (s *State) voteTimeout(ctx context.Context, view types.View) error {
	// Wait for view.
	if _, err := s.waitForView(ctx, view); err != nil {
		return err
	}
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if i.View() != view || i.TimeoutVote.IsPresent() {
			return nil
		}
		// If no PrepareQC was observed in this view, inherit the one from the
		// TimeoutQC that justified entering this view. Without this,
		// consecutive timeouts (e.g. offline leader) would lose the lock and
		// allow a conflicting proposal to be committed.
		pqc := i.PrepareQC
		if tqc, ok := i.TimeoutQC.Get(); ok && !pqc.IsPresent() {
			pqc = tqc.LatestPrepareQC()
		}
		v := types.NewFullTimeoutVote(s.cfg.Key, view, pqc)
		i.TimeoutVote = utils.Some(v)
		isend.Store(i)
	}
	return nil
}
