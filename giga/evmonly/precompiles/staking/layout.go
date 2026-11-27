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
	slotRedelegationEntryCounts     = 12
	slotRedelegationsByValDst       = 13
	slotUnbondingQueueBounds        = 14
	slotUnbondingQueue              = 15
	slotRedelegationQueueBounds     = 16
	slotRedelegationQueue           = 17
	slotValidatorQueueBounds        = 18
	slotValidatorQueue              = 19
	slotCurrentEpoch                = 20
	slotStagedQueueBounds           = 21
	slotStagedQueue                 = 22
	slotStagedDelegations           = 23
	slotStagedInbound               = 24
	slotStagedWork                  = 25
	slotParams                      = 26
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

// StakingStorage.StagedOperation members after its epoch, which is the
// completionTime member every queue entry begins with.
var (
	stagedOperationKind         = member{0, 8}
	stagedOperationDelegator    = member{0, 9}
	stagedOperationValidator    = member{1, 0}
	stagedOperationDstValidator = member{2, 0}
	stagedOperationAmount       = member{3, 0}
)

// How many slots each queue's entries take.
const (
	unbondingQueueEntrySlots    = 2
	redelegationQueueEntrySlots = 5
	validatorQueueEntrySlots    = 2
	stagedQueueEntrySlots       = 4
)

// StakingStorage.StagedDelegation members.
var (
	stagedSharesOut        = member{0, 0}
	stagedUndelegations    = member{1, 0}
	stagedRedelegationsOut = member{1, 8}
	stagedRedelegationsIn  = member{1, 16}
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
