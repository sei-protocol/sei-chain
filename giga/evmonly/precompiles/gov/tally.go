package gov

import (
	"math/big"
	"strconv"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Tally is the voting weight behind each option of a proposal.
type Tally struct {
	Yes, Abstain, No, NoWithVeto uint64
}

func (t Tally) data() tallyResultData {
	return tallyResultData{
		Yes:        strconv.FormatUint(t.Yes, 10),
		Abstain:    strconv.FormatUint(t.Abstain, 10),
		No:         strconv.FormatUint(t.No, 10),
		NoWithVeto: strconv.FormatUint(t.NoWithVeto, 10),
	}
}

// tally sums the weight of every voter's vote on proposal id.
func (c *Contract) tally(s store, id uint64) Tally {
	var t Tally
	for _, voter := range c.voters {
		switch int32(s.u64(voteSlot(id, voter.Address))) { //nolint:gosec // stored options are in [0, 4].
		case OptionYes:
			t.Yes += voter.Weight
		case OptionAbstain:
			t.Abstain += voter.Weight
		case OptionNo:
			t.No += voter.Weight
		case OptionNoWithVeto:
			t.NoWithVeto += voter.Weight
		}
	}
	return t
}

func (s store) finalTally(id uint64) Tally {
	return Tally{
		Yes:        s.u64(proposalSlot(id, fieldTallyYes)),
		Abstain:    s.u64(proposalSlot(id, fieldTallyAbstain)),
		No:         s.u64(proposalSlot(id, fieldTallyNo)),
		NoWithVeto: s.u64(proposalSlot(id, fieldTallyNoWithVeto)),
	}
}

func (w writer) setFinalTally(id uint64, t Tally) {
	w.setU64(proposalSlot(id, fieldTallyYes), t.Yes)
	w.setU64(proposalSlot(id, fieldTallyAbstain), t.Abstain)
	w.setU64(proposalSlot(id, fieldTallyNo), t.No)
	w.setU64(proposalSlot(id, fieldTallyNoWithVeto), t.NoWithVeto)
}

// Passes reports whether t passes under params out of a total voting weight,
// by the rules of x/gov's tally: quorum of total weight, then a veto share of
// the votes cast, then a yes share of the non-abstaining votes. Fractions are
// compared exactly.
func (p Params) Passes(t Tally, total uint64) bool {
	voted := new(big.Int).SetUint64(t.Yes)
	voted.Add(voted, new(big.Int).SetUint64(t.Abstain))
	voted.Add(voted, new(big.Int).SetUint64(t.No))
	voted.Add(voted, new(big.Int).SetUint64(t.NoWithVeto))
	if total == 0 || voted.Sign() == 0 {
		return false
	}
	one := DecOne.big()
	// voted / total < quorum
	if new(big.Int).Mul(voted, one).Cmp(new(big.Int).Mul(p.Quorum.big(), new(big.Int).SetUint64(total))) < 0 {
		return false
	}
	nonAbstain := new(big.Int).Sub(voted, new(big.Int).SetUint64(t.Abstain))
	if nonAbstain.Sign() == 0 {
		return false
	}
	// noWithVeto / voted > vetoThreshold
	if new(big.Int).Mul(new(big.Int).SetUint64(t.NoWithVeto), one).Cmp(new(big.Int).Mul(p.VetoThreshold.big(), voted)) > 0 {
		return false
	}
	// yes / (voted - abstain) > threshold
	return new(big.Int).Mul(new(big.Int).SetUint64(t.Yes), one).Cmp(new(big.Int).Mul(p.Threshold.big(), nonAbstain)) > 0
}

// EndBlock completes the upgrade plan due at this block, then ends every
// proposal whose voting period is over at this block's time, up to
// maxProposalsEndedPerBlock of them, in submission order. A passing proposal is
// then executed: a software upgrade replaces the upgrade plan and a
// cancellation clears it. An upgrade whose height is not above this block, or
// whose name was already completed, fails instead.
func (c *Contract) EndBlock(block precompiles.BlockContext, state precompiles.State) error {
	w := newWriter(c.addr, state)
	w.completeDuePlan(block.Number)
	start, tail := w.u64(slotQueueHead), w.u64(slotQueueTail)
	head := start
	for ended := 0; head < tail && ended < maxProposalsEndedPerBlock; ended++ {
		id := w.u64(queueSlot(head))
		if w.u64(proposalSlot(id, fieldVotingEndTime)) > block.Time {
			break
		}
		c.endProposal(w, id, block.Number)
		w.setU64(queueSlot(head), 0)
		head++
	}
	if head != start {
		w.setU64(slotQueueHead, head)
	}
	return nil
}

func (c *Contract) endProposal(w writer, id uint64, height uint64) {
	t := c.tally(w.store, id)
	w.setFinalTally(id, t)
	status := StatusRejected
	if c.params.Passes(t, c.total) {
		status = c.execute(w, id, height)
	}
	w.setU64(proposalSlot(id, fieldStatus), uint64(status)) //nolint:gosec // statuses are small and positive.
}

// execute applies passed proposal id and returns its final status.
func (c *Contract) execute(w writer, id uint64, height uint64) int32 {
	if w.u64(proposalSlot(id, fieldKind)) == kindCancelSoftwareUpgrade {
		w.clearPlan()
		return StatusPassed
	}
	plan := w.proposalPlan(id)
	if plan.Height <= height || w.u64(doneSlot(plan.Name)) != 0 {
		return StatusFailed
	}
	w.setPlan(plan, id)
	return StatusPassed
}
