package gov

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Address is the gov precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001006")

// Top-level slots of docs/giga-storage/GovStorage.sol.
const (
	slotProposalIDs          = 0
	slotParams               = 1
	slotPlan                 = 7
	slotProposals            = 11
	slotVotes                = 12
	slotDeposits             = 13
	slotDepositors           = 14
	slotProposalsVotedBy     = 15
	slotProposalsDepositedBy = 16
	slotVotingQueue          = 17
	slotDepositQueue         = 19
	slotAppliedPlanHeight    = 21
	slotVotersByProposal     = 22
	slotTallies              = 23
	slotTallyDeductions      = 24
)

// member is a struct member's position relative to the struct's base slot.
type member struct {
	slot   uint64
	offset int
}

// Proposal id counters, packed into slotProposalIDs.
var (
	lastProposalID     = member{0, 0}
	startingProposalID = member{0, 8}
)

// GovStorage.Tally members.
var (
	tallyYes        = member{0, 0}
	tallyAbstain    = member{1, 0}
	tallyNo         = member{2, 0}
	tallyNoWithVeto = member{3, 0}
	tallyPower      = member{4, 0}
	tallyCounting   = member{5, 0}
)

// GovStorage.Params members.
var (
	paramsVotingPeriod          = member{0, 0}
	paramsExpeditedVotingPeriod = member{0, 8}
	paramsMinDeposit            = member{1, 0}
	paramsMaxDepositPeriod      = member{2, 0}
	paramsMinExpeditedDeposit   = member{3, 0}
	paramsQuorum                = member{4, 0}
	paramsThreshold             = member{4, 8}
	paramsVetoThreshold         = member{4, 16}
	paramsExpeditedQuorum       = member{4, 24}
	paramsExpeditedThreshold    = member{5, 0}
)

// GovStorage.Plan members.
var (
	planName       = member{0, 0}
	planHeight     = member{1, 0}
	planInfo       = member{2, 0}
	planProposalID = member{3, 0}
)

// GovStorage.Proposal members.
var (
	proposalStatus          = member{0, 0}
	proposalIsExpedited     = member{0, 4}
	proposalSubmitTime      = member{0, 5}
	proposalDepositEndTime  = member{0, 13}
	proposalVotingStartTime = member{0, 21}
	proposalVotingEndTime   = member{1, 0}
	proposalTotalDeposit    = member{2, 0}
	proposalTallyYes        = member{3, 0}
	proposalTallyAbstain    = member{4, 0}
	proposalTallyNo         = member{5, 0}
	proposalTallyNoWithVeto = member{6, 0}
	proposalContent         = member{7, 0}
)

// GovStorage.WeightedVote members.
var (
	weightedVoteOption = member{0, 0}
	weightedVoteWeight = member{0, 4}
)

// DepositEscrow holds every deposit until its proposal settles.
var DepositEscrow = common.BytesToAddress(crypto.Keccak256(
	[]byte("sei.evmonly.escrow"), Address[:], []byte("deposit"),
)[12:])
