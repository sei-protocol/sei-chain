package staking

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Address is the staking precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001005")

// Top-level slots of docs/giga-storage/StakingStorage.sol.
const (
	slotLastValidators              = 0
	slotLastTotalPower              = 1
	slotEpochValidators             = 2
	slotNextSnapshotEpoch           = 3
	slotValidators                  = 4
	slotValidatorSet                = 5
	slotValidatorsByConsensusPubkey = 7
	slotValidatorsByPower           = 8
	slotDelegations                 = 9
	slotDelegationsByDelegator      = 10
	slotDelegationsByValidator      = 11
	slotUnbondingDelegations        = 12
	slotRedelegationEntryCounts     = 13
	slotRedelegationsByValDst       = 14
	slotUnbondingQueueBounds        = 15
	slotUnbondingQueue              = 16
	slotRedelegationQueueBounds     = 17
	slotRedelegationQueue           = 18
	slotValidatorQueueBounds        = 19
	slotValidatorQueue              = 20
)

// member is a struct member's position relative to the struct's base slot.
type member struct {
	slot   uint64
	offset int
}

// StakingStorage.Validator members.
var (
	validatorConsensusPubkey     = member{0, 0}
	validatorJailed              = member{1, 0}
	validatorStatus              = member{1, 1}
	validatorTokens              = member{2, 0}
	validatorDelegatorShares     = member{3, 0}
	validatorMoniker             = member{4, 0}
	validatorIdentity            = member{5, 0}
	validatorWebsite             = member{6, 0}
	validatorSecurityContact     = member{7, 0}
	validatorDetails             = member{8, 0}
	validatorUnbondingHeight     = member{9, 0}
	validatorUnbondingTime       = member{9, 8}
	validatorCommissionRate      = member{10, 0}
	validatorCommissionMaxRate   = member{11, 0}
	validatorCommissionMaxChange = member{12, 0}
	validatorCommissionUpdate    = member{13, 0}
	validatorMinSelfDelegation   = member{14, 0}
)

// validatorSlots is how many slots a StakingStorage.Validator takes.
const validatorSlots = 15

// StakingStorage.ValidatorPower members.
var (
	validatorPowerOperator = member{0, 0}
	validatorPowerPower    = member{0, 20}
)

// StakingStorage.UnbondingDelegationEntry members.
var (
	unbondingEntryCreationHeight = member{0, 0}
	unbondingEntryCompletionTime = member{0, 8}
	unbondingEntryInitialBalance = member{1, 0}
	unbondingEntryBalance        = member{2, 0}
)

// unbondingEntrySlots is how many slots a StakingStorage.UnbondingDelegationEntry takes.
const unbondingEntrySlots = 3

// StakingStorage.QueueBounds members.
var (
	queueHead = member{0, 0}
	queueTail = member{0, 8}
)

// queueCompletionTime is the completionTime member every queue entry begins with.
var queueCompletionTime = member{0, 0}

// StakingStorage.UnbondingQueueEntry members.
var (
	unbondingQueueDelegator = member{0, 8}
	unbondingQueueValidator = member{1, 0}
)

// StakingStorage.RedelegationQueueEntry members.
var (
	redelegationQueueDelegator      = member{0, 8}
	redelegationQueueCreationHeight = member{1, 0}
	redelegationQueueSrcValidator   = member{1, 8}
	redelegationQueueDstValidator   = member{2, 0}
	redelegationQueueInitialBalance = member{3, 0}
	redelegationQueueSharesDst      = member{4, 0}
)

// StakingStorage.ValidatorQueueEntry members.
var (
	validatorQueueUnbondingHeight = member{0, 8}
	validatorQueueOperator        = member{1, 0}
)

// How many slots each queue's entries take.
const (
	unbondingQueueEntrySlots    = 2
	redelegationQueueEntrySlots = 5
	validatorQueueEntrySlots    = 2
)

// BondedEscrow holds the tokens of bonded validators.
var BondedEscrow = escrow("bonded")

// NotBondedEscrow holds the tokens of unbonded and unbonding validators and the
// balance of every unbonding entry.
var NotBondedEscrow = escrow("not_bonded")

func escrow(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte("sei.evmonly.escrow"), Address[:], []byte(name))[12:])
}
