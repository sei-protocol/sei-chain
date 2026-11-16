package distribution

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Address is the distribution precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001007")

// Top-level slots of docs/giga-storage/DistributionStorage.sol.
const (
	slotCommunityPool                  = 0
	slotPreviousProposer               = 1
	slotValidatorOutstandingRewards    = 2
	slotDelegatorWithdrawAddr          = 3
	slotDelegatorStartingInfo          = 4
	slotValidatorHistoricalRewards     = 5
	slotValidatorCurrentRewards        = 6
	slotValidatorAccumulatedCommission = 7
	slotValidatorSlashEvents           = 8
	slotEpoch                          = 9
)

// Variables of DistributionStorage that share slotEpoch.
var (
	epochDAO            = member{slotEpoch, 0}
	nextSettlementEpoch = member{slotEpoch, 20}
)

// member is a struct member's position relative to the struct's base slot.
type member struct {
	slot   uint64
	offset int
}

// DistributionStorage.ValidatorHistoricalRewards members.
var (
	historicalCumulativeRewardRatio = member{0, 0}
	historicalReferenceCount        = member{1, 0}
)

// DistributionStorage.ValidatorCurrentRewards members.
var (
	currentRewardsRewards = member{0, 0}
	currentRewardsPeriod  = member{1, 0}
)

// DistributionStorage.DelegatorStartingInfo members.
var (
	startingInfoPreviousPeriod = member{0, 0}
	startingInfoHeight         = member{0, 8}
	startingInfoStake          = member{1, 0}
)

// DistributionStorage.ValidatorSlashEvent members.
var (
	slashEventHeight          = member{0, 0}
	slashEventValidatorPeriod = member{0, 8}
	slashEventFraction        = member{1, 0}
)

// Slot counts of DistributionStorage's structs.
const (
	historicalRewardsSlots = 2
	currentRewardsSlots    = 2
	startingInfoSlots      = 2
	slashEventSlots        = 10
)

// DistributionEscrow holds every validator's outstanding rewards.
var DistributionEscrow = escrow("distribution")

// FeeCollector is every block's coinbase, which receives the block's fees. It
// is V2's coinbase, the EVM address of the fee_collector module account: a
// CREATE address of the ecrecover precompile, which no key controls.
var FeeCollector = crypto.CreateAddress(common.BytesToAddress([]byte{1}), 42)

func escrow(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte("sei.evmonly.escrow"), Address[:], []byte(name))[12:])
}
