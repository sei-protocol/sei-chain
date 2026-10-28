package gov

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// Params is GovStorage.Params. Periods are nanoseconds, deposits are wei, and
// ratios are 1e18-scaled decimals.
type Params struct {
	VotingPeriod          uint64   `json:"voting_period"`
	ExpeditedVotingPeriod uint64   `json:"expedited_voting_period"`
	MinDeposit            *big.Int `json:"min_deposit"`
	MaxDepositPeriod      uint64   `json:"max_deposit_period"`
	MinExpeditedDeposit   *big.Int `json:"min_expedited_deposit"`
	Quorum                uint64   `json:"quorum"`
	Threshold             uint64   `json:"threshold"`
	VetoThreshold         uint64   `json:"veto_threshold"`
	ExpeditedQuorum       uint64   `json:"expedited_quorum"`
	ExpeditedThreshold    uint64   `json:"expedited_threshold"`
}

// proposal is GovStorage.Proposal without its content, which is read and
// written separately because only submission and the proposal queries need it.
type proposal struct {
	status          int32
	isExpedited     bool
	submitTime      uint64
	depositEndTime  uint64
	votingStartTime uint64
	votingEndTime   uint64
	totalDeposit    *big.Int
	tallyYes        *big.Int
	tallyAbstain    *big.Int
	tallyNo         *big.Int
	tallyNoWithVeto *big.Int
}

// Plan is GovStorage.Plan, the scheduled software upgrade.
type Plan struct {
	Name       string
	Height     int64
	Info       string
	ProposalID uint64
}

// weightedVote is GovStorage.WeightedVote.
type weightedVote struct {
	option int32
	weight uint64
}

// store is GovStorage at the gov precompile's address.
type store struct {
	solstorage.Store
}

func newStore(state precompiles.State) store {
	return store{solstorage.Store{State: state, Address: Address}}
}

func at(base common.Hash, m member) common.Hash {
	return solstorage.Offset(base, m.slot)
}

// lastProposalID returns the highest proposal id assigned, or 0 if none has been.
func (s store) lastProposalID() uint64 {
	return s.Uint64(at(solstorage.Slot(slotProposalIDs), lastProposalID), lastProposalID.offset)
}

// startingProposalID returns the lowest proposal id gov assigns.
func (s store) startingProposalID() uint64 {
	return max(s.Uint64(at(solstorage.Slot(slotProposalIDs), startingProposalID), startingProposalID.offset), 1)
}

func (s store) setStartingProposalID(id uint64) {
	s.SetUint64(at(solstorage.Slot(slotProposalIDs), startingProposalID), startingProposalID.offset, id)
}

// nextProposalID assigns and returns the next proposal id.
func (s store) nextProposalID() uint64 {
	id := max(s.lastProposalID()+1, s.startingProposalID())
	s.SetUint64(at(solstorage.Slot(slotProposalIDs), lastProposalID), lastProposalID.offset, id)
	return id
}

func (s store) params() Params {
	base := solstorage.Slot(slotParams)
	return Params{
		VotingPeriod:          s.Uint64(at(base, paramsVotingPeriod), paramsVotingPeriod.offset),
		ExpeditedVotingPeriod: s.Uint64(at(base, paramsExpeditedVotingPeriod), paramsExpeditedVotingPeriod.offset),
		MinDeposit:            s.Uint256(at(base, paramsMinDeposit)),
		MaxDepositPeriod:      s.Uint64(at(base, paramsMaxDepositPeriod), paramsMaxDepositPeriod.offset),
		MinExpeditedDeposit:   s.Uint256(at(base, paramsMinExpeditedDeposit)),
		Quorum:                s.Uint64(at(base, paramsQuorum), paramsQuorum.offset),
		Threshold:             s.Uint64(at(base, paramsThreshold), paramsThreshold.offset),
		VetoThreshold:         s.Uint64(at(base, paramsVetoThreshold), paramsVetoThreshold.offset),
		ExpeditedQuorum:       s.Uint64(at(base, paramsExpeditedQuorum), paramsExpeditedQuorum.offset),
		ExpeditedThreshold:    s.Uint64(at(base, paramsExpeditedThreshold), paramsExpeditedThreshold.offset),
	}
}

func (s store) setParams(p Params) {
	base := solstorage.Slot(slotParams)
	s.SetUint64(at(base, paramsVotingPeriod), paramsVotingPeriod.offset, p.VotingPeriod)
	s.SetUint64(at(base, paramsExpeditedVotingPeriod), paramsExpeditedVotingPeriod.offset, p.ExpeditedVotingPeriod)
	s.SetUint256(at(base, paramsMinDeposit), p.MinDeposit)
	s.SetUint64(at(base, paramsMaxDepositPeriod), paramsMaxDepositPeriod.offset, p.MaxDepositPeriod)
	s.SetUint256(at(base, paramsMinExpeditedDeposit), p.MinExpeditedDeposit)
	s.SetUint64(at(base, paramsQuorum), paramsQuorum.offset, p.Quorum)
	s.SetUint64(at(base, paramsThreshold), paramsThreshold.offset, p.Threshold)
	s.SetUint64(at(base, paramsVetoThreshold), paramsVetoThreshold.offset, p.VetoThreshold)
	s.SetUint64(at(base, paramsExpeditedQuorum), paramsExpeditedQuorum.offset, p.ExpeditedQuorum)
	s.SetUint64(at(base, paramsExpeditedThreshold), paramsExpeditedThreshold.offset, p.ExpeditedThreshold)
}

func proposalBase(id uint64) common.Hash {
	return solstorage.MapUint64(solstorage.Slot(slotProposals), id)
}

// proposal returns the proposal with id, reporting whether it exists. A
// proposal dropped at the end of its deposit period no longer exists.
func (s store) proposal(id uint64) (proposal, bool) {
	if id < s.startingProposalID() || id > s.lastProposalID() {
		return proposal{}, false
	}
	base := proposalBase(id)
	status := s.Int32(at(base, proposalStatus), proposalStatus.offset)
	if status == 0 {
		return proposal{}, false
	}
	return proposal{
		status:          status,
		isExpedited:     s.Bool(at(base, proposalIsExpedited), proposalIsExpedited.offset),
		submitTime:      s.Uint64(at(base, proposalSubmitTime), proposalSubmitTime.offset),
		depositEndTime:  s.Uint64(at(base, proposalDepositEndTime), proposalDepositEndTime.offset),
		votingStartTime: s.Uint64(at(base, proposalVotingStartTime), proposalVotingStartTime.offset),
		votingEndTime:   s.Uint64(at(base, proposalVotingEndTime), proposalVotingEndTime.offset),
		totalDeposit:    s.Uint256(at(base, proposalTotalDeposit)),
		tallyYes:        s.Uint256(at(base, proposalTallyYes)),
		tallyAbstain:    s.Uint256(at(base, proposalTallyAbstain)),
		tallyNo:         s.Uint256(at(base, proposalTallyNo)),
		tallyNoWithVeto: s.Uint256(at(base, proposalTallyNoWithVeto)),
	}, true
}

func (s store) setProposal(id uint64, p proposal) {
	base := proposalBase(id)
	s.SetInt32(at(base, proposalStatus), proposalStatus.offset, p.status)
	s.SetBool(at(base, proposalIsExpedited), proposalIsExpedited.offset, p.isExpedited)
	s.SetUint64(at(base, proposalSubmitTime), proposalSubmitTime.offset, p.submitTime)
	s.SetUint64(at(base, proposalDepositEndTime), proposalDepositEndTime.offset, p.depositEndTime)
	s.SetUint64(at(base, proposalVotingStartTime), proposalVotingStartTime.offset, p.votingStartTime)
	s.SetUint64(at(base, proposalVotingEndTime), proposalVotingEndTime.offset, p.votingEndTime)
	s.SetUint256(at(base, proposalTotalDeposit), p.totalDeposit)
	s.SetUint256(at(base, proposalTallyYes), p.tallyYes)
	s.SetUint256(at(base, proposalTallyAbstain), p.tallyAbstain)
	s.SetUint256(at(base, proposalTallyNo), p.tallyNo)
	s.SetUint256(at(base, proposalTallyNoWithVeto), p.tallyNoWithVeto)
}

// deleteProposal zeroes proposal id and its content.
func (s store) deleteProposal(id uint64) {
	s.setProposal(id, proposal{})
	s.setProposalContent(id, 0, nil)
}

// proposalContent returns proposal id's content type and its ABI encoding.
func (s store) proposalContent(id uint64) (uint8, []byte) {
	base := proposalBase(id)
	return s.Uint8(at(base, proposalContentType), proposalContentType.offset), []byte(s.String(at(base, proposalContent)))
}

func (s store) setProposalContent(id uint64, contentType uint8, content []byte) {
	base := proposalBase(id)
	s.SetUint8(at(base, proposalContentType), proposalContentType.offset, contentType)
	s.SetString(at(base, proposalContent), string(content))
}

func voteBase(id uint64, voter common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapUint64(solstorage.Slot(slotVotes), id), voter)
}

// vote returns voter's options on proposal id, empty when it has not voted.
func (s store) vote(id uint64, voter common.Address) []weightedVote {
	base := voteBase(id, voter)
	n := s.Uint64(base, 0)
	data := solstorage.ArrayData(base)
	out := make([]weightedVote, n)
	for i := range out {
		slot := solstorage.Offset(data, uint64(i)) //nolint:gosec // i is below n
		out[i] = weightedVote{
			option: s.Int32(slot, weightedVoteOption.offset),
			weight: s.Uint64(slot, weightedVoteWeight.offset),
		}
	}
	return out
}

// setVote replaces voter's options on proposal id, zeroing elements the new
// array no longer covers.
func (s store) setVote(id uint64, voter common.Address, options []weightedVote) {
	base := voteBase(id, voter)
	old := s.Uint64(base, 0)
	data := solstorage.ArrayData(base)
	for i, o := range options {
		slot := solstorage.Offset(data, uint64(i)) //nolint:gosec // i is non-negative
		s.SetWord(slot, common.Hash{})
		s.SetInt32(slot, weightedVoteOption.offset, o.option)
		s.SetUint64(slot, weightedVoteWeight.offset, o.weight)
	}
	for i := uint64(len(options)); i < old; i++ {
		s.SetWord(solstorage.Offset(data, i), common.Hash{})
	}
	s.SetWord(base, solstorage.Slot(uint64(len(options))))
}

func depositSlot(id uint64, depositor common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapUint64(solstorage.Slot(slotDeposits), id), depositor)
}

func (s store) deposit(id uint64, depositor common.Address) *big.Int {
	return s.Uint256(depositSlot(id, depositor))
}

func (s store) setDeposit(id uint64, depositor common.Address, amount *big.Int) {
	s.SetUint256(depositSlot(id, depositor), amount)
}

func (s store) depositors(id uint64) solstorage.Set {
	return solstorage.Set{Store: s.Store, Base: solstorage.MapUint64(solstorage.Slot(slotDepositors), id)}
}

func (s store) votersByProposal(id uint64) solstorage.Set {
	return solstorage.Set{Store: s.Store, Base: solstorage.MapUint64(solstorage.Slot(slotVotersByProposal), id)}
}

func (s store) proposalsVotedBy(voter common.Address) solstorage.Set {
	return solstorage.Set{Store: s.Store, Base: solstorage.MapAddress(solstorage.Slot(slotProposalsVotedBy), voter)}
}

func (s store) proposalsDepositedBy(depositor common.Address) solstorage.Set {
	return solstorage.Set{Store: s.Store, Base: solstorage.MapAddress(solstorage.Slot(slotProposalsDepositedBy), depositor)}
}

// proposalQueue holds proposals by when they end, lowest id first among
// proposals ending together.
type proposalQueue struct {
	heap solstorage.Heap
}

func (s store) votingQueue() proposalQueue {
	return proposalQueue{solstorage.NewHeap(s.Store, solstorage.Slot(slotVotingQueue))}
}

func (s store) depositQueue() proposalQueue {
	return proposalQueue{solstorage.NewHeap(s.Store, solstorage.Slot(slotDepositQueue))}
}

// queueKey orders a proposal queue by end time, then id.
func queueKey(end, id uint64) common.Hash {
	var key common.Hash
	binary.BigEndian.PutUint64(key[16:24], end)
	binary.BigEndian.PutUint64(key[24:], id)
	return key
}

func (q proposalQueue) Len() uint64 {
	return q.heap.Len()
}

// id returns the proposal id at index i of the queue, which must be below Len.
func (q proposalQueue) id(i uint64) uint64 {
	return solstorage.MemberUint64(q.heap.At(i))
}

func (q proposalQueue) contains(id, end uint64) bool {
	return q.heap.Contains(queueKey(end, id))
}

func (q proposalQueue) add(id, end uint64) {
	q.heap.Push(queueKey(end, id))
}

func (q proposalQueue) remove(id, end uint64) {
	q.heap.Remove(queueKey(end, id))
}

// due returns the id and end time of the queued proposal that ends first,
// lowest id first, if it ends at or before now.
func (q proposalQueue) due(now uint64) (id, end uint64, ok bool) {
	if q.Len() == 0 {
		return 0, 0, false
	}
	key := q.heap.At(0)
	end = binary.BigEndian.Uint64(key[16:24])
	if end > now {
		return 0, 0, false
	}
	return solstorage.MemberUint64(key), end, true
}

// tallyState is a tally in progress, in 1e18-scaled voting power.
type tallyState struct {
	results  map[int32]*big.Int
	power    *big.Int
	counting bool
}

func tallyBase(id uint64) common.Hash {
	return solstorage.MapUint64(solstorage.Slot(slotTallies), id)
}

func (s store) tally(id uint64) tallyState {
	base := tallyBase(id)
	return tallyState{
		results: map[int32]*big.Int{
			OptionYes:        s.Uint256(at(base, tallyYes)),
			OptionAbstain:    s.Uint256(at(base, tallyAbstain)),
			OptionNo:         s.Uint256(at(base, tallyNo)),
			OptionNoWithVeto: s.Uint256(at(base, tallyNoWithVeto)),
		},
		power:    s.Uint256(at(base, tallyPower)),
		counting: s.tallyCounting(id),
	}
}

func (s store) setTally(id uint64, t tallyState) {
	base := tallyBase(id)
	s.SetUint256(at(base, tallyYes), t.results[OptionYes])
	s.SetUint256(at(base, tallyAbstain), t.results[OptionAbstain])
	s.SetUint256(at(base, tallyNo), t.results[OptionNo])
	s.SetUint256(at(base, tallyNoWithVeto), t.results[OptionNoWithVeto])
	s.SetUint256(at(base, tallyPower), t.power)
	s.SetBool(at(base, tallyCounting), tallyCounting.offset, t.counting)
}

// tallyCounting reports whether proposal id's votes are being counted.
func (s store) tallyCounting(id uint64) bool {
	return s.Bool(at(tallyBase(id), tallyCounting), tallyCounting.offset)
}

func tallyDeductionSlot(id uint64, validator common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapUint64(solstorage.Slot(slotTallyDeductions), id), validator)
}

// tallyDeduction returns validator's delegator shares whose delegators voted
// on proposal id themselves, as counted so far.
func (s store) tallyDeduction(id uint64, validator common.Address) *big.Int {
	return s.Uint256(tallyDeductionSlot(id, validator))
}

func (s store) setTallyDeduction(id uint64, validator common.Address, shares *big.Int) {
	s.SetUint256(tallyDeductionSlot(id, validator), shares)
}

func (s store) plan() Plan {
	base := solstorage.Slot(slotPlan)
	return Plan{
		Name:       s.String(at(base, planName)),
		Height:     s.Int64(at(base, planHeight), planHeight.offset),
		Info:       s.String(at(base, planInfo)),
		ProposalID: s.Uint64(at(base, planProposalID), planProposalID.offset),
	}
}

func (s store) setPlan(p Plan) {
	base := solstorage.Slot(slotPlan)
	s.SetString(at(base, planName), p.Name)
	s.SetInt64(at(base, planHeight), planHeight.offset, p.Height)
	s.SetString(at(base, planInfo), p.Info)
	s.SetUint64(at(base, planProposalID), planProposalID.offset, p.ProposalID)
}

// appliedPlanHeight returns the height plan name was applied at, 0 if never.
func (s store) appliedPlanHeight(name string) int64 {
	return s.Int64(solstorage.MapString(solstorage.Slot(slotAppliedPlanHeight), name), 0)
}
