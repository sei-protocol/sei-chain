package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// commitProgress owns the admitted CommitQC log and its durable tip.
// push advances only the admitted queue; only restore and markPersisted
// write persistedCommitQC.
type commitProgress struct {
	qcs               *queue[types.RoadIndex, *types.CommitQC]
	persistedCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]]
}

// push inserts qc at qcs.next. Returns false if idx is not the tip
// (race / already applied). Does not write persistedCommitQC.
func (c *commitProgress) push(qc *types.CommitQC) bool {
	idx := qc.Proposal().Index()
	if idx != c.qcs.next {
		return false
	}
	c.qcs.pushBack(qc)
	metrics.ObserveCommitQC(qc)
	return true
}

// markPersisted publishes the latest durably persisted CommitQC.
func (c *commitProgress) markPersisted(qc *types.CommitQC) {
	c.persistedCommitQC.Store(utils.Some(qc))
}
