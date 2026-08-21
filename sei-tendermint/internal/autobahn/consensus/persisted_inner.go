package consensus

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// persistedInner holds the persistable consensus state.
// Embedded in inner so fields are promoted; the persister (writer) lives on State.
//
// # What We Persist
//
// All fields are persisted atomically in a single A/B file pair (inner_a.pb/inner_b.pb):
//   - Index: current view RoadIndex used to validate the WAL against ConsensusSpec
//   - PrepareQC: needed for timeoutVote on restart
//   - TimeoutQC: justified entering the current view number
//   - CommitVote, PrepareVote, TimeoutVote: this node's votes for the current view
//
// Runtime CommitQC + next-view epoch live on inner.spec (ConsensusSpec), not here.
//
// # Why We Persist
//
// Safety: Votes prevent double-voting on restart — a critical safety property.
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
//   - Only D, E have those votes — not enough for quorum
//   - D, E are stuck at view (5, 1), cannot advance
//
// With QC persistence:
//   - A, B, C have TimeoutQC(5, 2) persisted — justifies view (5, 3)
//   - D, E have TimeoutQC(5, 1) persisted — justifies view (5, 2)
//   - On restart, everyone rebroadcasts their persisted QCs
//   - D, E are already at (5, 2), they broadcast TimeoutQC(5, 1) (helps no one)
//   - A, B, C broadcast TimeoutQC(5, 2)
//   - D, E receive TimeoutQC(5, 2), can now advance to (5, 3)
//   - Everyone converges to the highest view
//
// # Rebroadcasting
//
// On restart, the consensus layer propagates loaded state to output watches,
// which triggers rebroadcasting to peers:
//   - Votes (prepareVote, commitVote, timeoutVote): YES — rebroadcast via sendUpdates
//   - TimeoutQC: YES — rebroadcast via myTimeoutQC watch
//   - CommitQC: NO — used locally for view justification but not rebroadcast;
//     the runtime tip comes from ConsensusSpec, while the WAL stores only
//     Index. CommitQCs are served via StreamCommitQCs from the data
//     layer. TODO: consider rebroadcasting CommitQC on restart to help peers
//     sync faster after cluster-wide outages.
type persistedInner struct {
	Index     types.RoadIndex
	PrepareQC utils.Option[*types.PrepareQC]
	TimeoutQC utils.Option[*types.TimeoutQC]

	CommitVote  utils.Option[*types.Signed[*types.CommitVote]]
	PrepareVote utils.Option[*types.Signed[*types.PrepareVote]]
	TimeoutVote utils.Option[*types.FullTimeoutVote]
}

// Verify checks internal consistency and cryptographic signatures of persisted state.
// Returns error on corrupt state. Index is checked against spec by newInner first.
// PrepareQC and TimeoutQC may be assembled from any committee replicas; CommitVote,
// PrepareVote, and TimeoutVote must be this node's.
func (p *persistedInner) Verify(spec types.ConsensusSpec, self types.PublicKey) error {
	ep := spec.Epoch
	// TimeoutQC index must equal NextIndexOpt(CommitQC) (i.e., CommitQC.Index+1, or 0 if missing).
	// Since we persist the entire inner state atomically, a mismatched index is always corrupt.
	if tqc, ok := p.TimeoutQC.Get(); ok {
		tqcIndex := tqc.View().Index
		expectedIndex := spec.Index()
		if tqcIndex != expectedIndex {
			return fmt.Errorf("corrupt persisted state: TimeoutQC has index %d but expected %d", tqcIndex, expectedIndex)
		}
		if err := tqc.Verify(ep, spec.CommitQC); err != nil {
			return fmt.Errorf("corrupt persisted state: TimeoutQC failed verification: %w", err)
		}
	}

	vs := types.ViewSpec{ConsensusSpec: spec, TimeoutQC: p.TimeoutQC}
	currentView := vs.View()

	// checkViewAndSig validates that a persisted field has the current view and a valid signature.
	// Since inner is persisted atomically, any view mismatch indicates corrupt state.
	checkViewAndSig := func(name string, view types.View, verifyErr error) error {
		if view != currentView {
			return fmt.Errorf("corrupt persisted state: %s has view %v but current view is %v", name, view, currentView)
		}
		if verifyErr != nil {
			return fmt.Errorf("corrupt persisted state: %s failed verification: %w", name, verifyErr)
		}
		return nil
	}

	// PrepareQC is required when CommitVote is present (CommitVote requires PrepareQC justification).
	if pqc, ok := p.PrepareQC.Get(); ok {
		if err := checkViewAndSig("PrepareQC", pqc.Proposal().View(), pqc.Verify(ep)); err != nil {
			return err
		}
	} else if p.CommitVote.IsPresent() {
		return fmt.Errorf("corrupt persisted state: CommitVote present without PrepareQC")
	}
	if v, ok := p.CommitVote.Get(); ok {
		if err := checkViewAndSig("CommitVote", v.Msg().Proposal().View(), verifyLocalSig(self, v)); err != nil {
			return err
		}
	}
	if v, ok := p.PrepareVote.Get(); ok {
		if err := checkViewAndSig("PrepareVote", v.Msg().Proposal().View(), verifyLocalSig(self, v)); err != nil {
			return err
		}
	}
	if v, ok := p.TimeoutVote.Get(); ok {
		if err := checkViewAndSig("TimeoutVote", v.View(), verifyLocalTimeoutVote(self, v, ep)); err != nil {
			return err
		}
	}
	return nil
}

func verifyLocalSig[T types.Msg](self types.PublicKey, v *types.Signed[T]) error {
	if v.Key() != self {
		return fmt.Errorf("signed by %q, not this node", v.Key())
	}
	return v.VerifySig()
}

func verifyLocalTimeoutVote(self types.PublicKey, v *types.FullTimeoutVote, ep *types.Epoch) error {
	if v.Vote().Key() != self {
		return fmt.Errorf("signed by %q, not this node", v.Vote().Key())
	}
	return v.Verify(ep)
}

// innerProtoConv is a protobuf converter for persistedInner.
var innerProtoConv = protoutils.Conv[*persistedInner, *pb.PersistedInner]{
	Encode: func(m *persistedInner) *pb.PersistedInner {
		p := &pb.PersistedInner{}
		if m.Index != 0 {
			v := uint64(m.Index)
			p.Index = &v
		}
		if v, ok := m.PrepareQC.Get(); ok {
			p.PrepareQc = types.PrepareQCConv.Encode(v)
		}
		if v, ok := m.TimeoutQC.Get(); ok {
			p.TimeoutQc = types.TimeoutQCConv.Encode(v)
		}
		if v, ok := m.CommitVote.Get(); ok {
			p.CommitVoteV2 = types.SignedCommitVoteConv.Encode(v)
		}
		if v, ok := m.PrepareVote.Get(); ok {
			p.PrepareVoteV2 = types.SignedPrepareVoteConv.Encode(v)
		}
		if v, ok := m.TimeoutVote.Get(); ok {
			p.TimeoutVote = types.FullTimeoutVoteConv.Encode(v)
		}
		return p
	},
	Decode: func(p *pb.PersistedInner) (*persistedInner, error) {
		m := &persistedInner{}
		if p.Index != nil {
			m.Index = types.RoadIndex(*p.Index)
		}
		if p.PrepareQc != nil {
			v, err := types.PrepareQCConv.Decode(p.PrepareQc)
			if err != nil {
				return nil, fmt.Errorf("prepare_qc: %w", err)
			}
			m.PrepareQC = utils.Some(v)
		}
		if p.TimeoutQc != nil {
			v, err := types.TimeoutQCConv.Decode(p.TimeoutQc)
			if err != nil {
				return nil, fmt.Errorf("timeout_qc: %w", err)
			}
			m.TimeoutQC = utils.Some(v)
		}
		if p.CommitVoteV2 != nil {
			v, err := types.SignedCommitVoteConv.Decode(p.CommitVoteV2)
			if err != nil {
				return nil, fmt.Errorf("commit_vote: %w", err)
			}
			m.CommitVote = utils.Some(v)
		}
		if p.PrepareVoteV2 != nil {
			v, err := types.SignedPrepareVoteConv.Decode(p.PrepareVoteV2)
			if err != nil {
				return nil, fmt.Errorf("prepare_vote: %w", err)
			}
			m.PrepareVote = utils.Some(v)
		}
		if p.TimeoutVote != nil {
			v, err := types.FullTimeoutVoteConv.Decode(p.TimeoutVote)
			if err != nil {
				return nil, fmt.Errorf("timeout_vote: %w", err)
			}
			m.TimeoutVote = utils.Some(v)
		}
		return m, nil
	},
}
