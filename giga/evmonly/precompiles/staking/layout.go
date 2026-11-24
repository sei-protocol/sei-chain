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
	slotValidators                  = 3
	slotValidatorSet                = 4
	slotValidatorsByConsensusPubkey = 6
	slotValidatorsByPower           = 7
	slotDelegations                 = 8
	slotDelegationsByDelegator      = 9
	slotDelegationsByValidator      = 10
	slotUnbondingDelegations        = 11
	slotUnbondingQueueBounds        = 12
	slotUnbondingQueue              = 13
	slotValidatorQueueBounds        = 14
	slotValidatorQueue              = 15
	slotCurrentEpoch                = 16
	slotStagedQueueBounds           = 17
	slotStagedQueue                 = 18
	slotStagedDelegations           = 19
	slotStagedInbound               = 20
	slotParams                      = 21
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

// StakingStorage.EpochValidator members.
var (
	epochValidatorOperator        = member{0, 0}
	epochValidatorPower           = member{0, 20}
	epochValidatorConsensusPubkey = member{1, 0}
)

// epochValidatorSlots is how many slots a StakingStorage.EpochValidator takes.
const epochValidatorSlots = 2

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

// StakingStorage.ValidatorQueueEntry members.
var (
	validatorQueueUnbondingHeight = member{0, 8}
	validatorQueueOperator        = member{1, 0}
)

// StakingStorage.StagedOperation members after its epoch, which is the
// completionTime member every queue entry begins with.
var (
	stagedOperationKind      = member{0, 8}
	stagedOperationDelegator = member{0, 9}
	stagedOperationValidator = member{1, 0}
	stagedOperationAmount    = member{2, 0}
)

// How many slots each queue's entries take.
const (
	unbondingQueueEntrySlots = 2
	validatorQueueEntrySlots = 2
	stagedQueueEntrySlots    = 3
)

// StakingStorage.StagedDelegation members.
var (
	stagedSharesOut     = member{0, 0}
	stagedUndelegations = member{1, 0}
)

// stagedDelegationSlots is how many slots a StakingStorage.StagedDelegation takes.
const stagedDelegationSlots = 2

// StakingStorage.Params members.
var (
	paramsUnbondingTime                      = member{0, 0}
	paramsMaxValidators                      = member{0, 8}
	paramsMaxEntries                         = member{0, 12}
	paramsMaxVotingPowerRatio                = member{1, 0}
	paramsMaxVotingPowerEnforcementThreshold = member{2, 0}
)

// BondedEscrow holds the tokens of bonded validators.
var BondedEscrow = escrow("bonded")

// NotBondedEscrow holds the tokens of unbonded and unbonding validators and the
// balance of every unbonding entry.
var NotBondedEscrow = escrow("not_bonded")

// StagedEscrow holds the tokens of staged delegations.
var StagedEscrow = escrow("staged")

func escrow(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte("sei.evmonly.escrow"), Address[:], []byte(name))[12:])
}
