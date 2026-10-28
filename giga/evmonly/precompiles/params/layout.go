package params

import (
	"github.com/ethereum/go-ethereum/common"
)

// Address is the params precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001013")

// Top-level slots of docs/giga-storage/ParamsStorage.sol.
const (
	slotGov = 0
)

// member is a struct member's position relative to the struct's base slot.
type member struct {
	slot   uint64
	offset int
}

// ParamsStorage.Gov members.
var (
	govMaxDepositPeriod      = member{0, 0}
	govVotingPeriod          = member{0, 8}
	govExpeditedVotingPeriod = member{0, 16}
	govMinDeposit            = member{1, 0}
	govMinExpeditedDeposit   = member{2, 0}
	govQuorum                = member{3, 0}
	govThreshold             = member{4, 0}
	govVetoThreshold         = member{5, 0}
	govExpeditedQuorum       = member{6, 0}
	govExpeditedThreshold    = member{7, 0}
)
