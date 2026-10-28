package gov

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Address is the gov precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001006")

// Top-level slots of docs/giga-storage/GovStorage.sol.
const (
	slotProposalIDs      = 0
	slotProposals        = 1
	slotVotes            = 2
	slotDeposits         = 3
	slotDepositors       = 4
	slotVotingQueue      = 5
	slotDepositQueue     = 7
	slotVotersByProposal = 9
	slotTallies          = 10
	slotTallyDeductions  = 11
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

// GovStorage.Proposal members.
var (
	proposalStatus          = member{0, 0}
	proposalIsExpedited     = member{0, 4}
	proposalContentType     = member{0, 5}
	proposalSubmitTime      = member{0, 6}
	proposalDepositEndTime  = member{0, 14}
	proposalVotingStartTime = member{0, 22}
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
