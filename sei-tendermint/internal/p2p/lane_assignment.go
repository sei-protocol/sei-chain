package p2p

import (
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneAssigner picks the lane that serves an EVM sender, among the lanes of the
// committee it was built for. Implementations are immutable and safe for
// concurrent use.
type laneAssigner interface {
	// Lane returns the validator of the first lane, in this assigner's order for
	// sender, that reachable accepts, or None when reachable accepts no lane.
	Lane(sender common.Address, reachable func(atypes.PublicKey) bool) utils.Option[atypes.PublicKey]
	// Serves reports whether this assigner, built for one committee, assigns the
	// same lanes for committee c.
	Serves(c *atypes.Committee) bool
}

// committeeLaneAssigner is a laneAssigner together with the committee it was built for.
type committeeLaneAssigner struct {
	committee *atypes.Committee
	assigner  laneAssigner
}

// laneAssignments holds the laneAssigner of the latest committee it was asked
// about. It is safe for concurrent use.
type laneAssignments struct {
	build  func(*atypes.Committee) laneAssigner
	latest atomic.Pointer[committeeLaneAssigner]
}

// newLaneAssignments returns a laneAssignments that builds each committee's
// laneAssigner with build.
func newLaneAssignments(build func(*atypes.Committee) laneAssigner) *laneAssignments {
	return &laneAssignments{build: build}
}

// For returns the laneAssigner of committee c.
func (a *laneAssignments) For(c *atypes.Committee) laneAssigner {
	latest := a.latest.Load()
	if latest != nil && latest.committee == c {
		return latest.assigner
	}
	next := &committeeLaneAssigner{committee: c}
	if latest != nil && latest.assigner.Serves(c) {
		// A committee that the assigner serves keeps that assigner, so senders keep
		// their lanes and the next lookup takes the pointer fast path.
		next.assigner = latest.assigner
	} else {
		next.assigner = a.build(c)
	}
	// Concurrent callers that see the same new committee may each build an
	// assigner. The assigners are equal, so whichever Store lands last is correct.
	// A caller that holds an older committee may replace a newer entry. Each
	// caller still gets the assigner of its own committee, and the next caller
	// with the newer committee builds again.
	a.latest.Store(next)
	return next.assigner
}
