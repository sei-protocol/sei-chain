package distribution

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// maxSlashEvents is the most slash events a validator's stored list may hold.
// It is far above any count slashing can produce, and bounds a corrupt length.
const maxSlashEvents = 1 << 32

var errInvalidStoredSlashEvents = errors.New("invalid stored slash events")

// historicalRewards is a DistributionStorage.ValidatorHistoricalRewards: the
// 1e18-scaled reward per token accumulated through a period, and how many
// delegations and slash events still refer to it.
type historicalRewards struct {
	cumulativeRewardRatio *big.Int
	referenceCount        uint32
}

// currentRewards is a DistributionStorage.ValidatorCurrentRewards: the wei
// credited to a validator's delegators in its open period.
type currentRewards struct {
	rewards *big.Int
	period  uint64
}

// startingInfo is a DistributionStorage.DelegatorStartingInfo: the period a
// delegation's rewards start after, the height it was last set at, and its
// stake in wei then.
type startingInfo struct {
	previousPeriod uint64
	height         uint64
	stake          *big.Int
}

// slashEvent is a DistributionStorage.ValidatorSlashEvent: the height of a
// slash, the validator period it ended, and the 1e18-scaled fraction slashed.
type slashEvent struct {
	height          uint64
	validatorPeriod uint64
	fraction        *big.Int
}

// store is DistributionStorage at the distribution precompile's address.
type store struct {
	solstorage.Store
}

func newStore(state precompiles.State) store {
	return store{solstorage.Store{State: state, Address: Address}}
}

func at(base common.Hash, m member) common.Hash {
	return solstorage.Offset(base, m.slot)
}

// clear zeroes the n slots of the struct at base.
func (s store) clear(base common.Hash, n uint64) {
	for i := uint64(0); i < n; i++ {
		s.SetWord(solstorage.Offset(base, i), common.Hash{})
	}
}

// params returns distribution's params from the params precompile.
func (s store) params() chainparams.Distribution {
	return chainparams.ReadDistribution(s.State)
}

// epochDAO returns the DAO address fixed for the current epoch.
func (s store) epochDAO() common.Address {
	return s.AddressAt(solstorage.Slot(epochDAO.slot), epochDAO.offset)
}

func (s store) setEpochDAO(dao common.Address) {
	s.SetAddressAt(solstorage.Slot(epochDAO.slot), epochDAO.offset, dao)
}

// nextSettlementEpoch returns the epoch the next settlement pays.
func (s store) nextSettlementEpoch() uint64 {
	return s.Uint64(solstorage.Slot(nextSettlementEpoch.slot), nextSettlementEpoch.offset)
}

func (s store) setNextSettlementEpoch(epoch uint64) {
	s.SetUint64(solstorage.Slot(nextSettlementEpoch.slot), nextSettlementEpoch.offset, epoch)
}

// communityPool returns the community pool in wei.
func (s store) communityPool() *big.Int {
	return s.Uint256(solstorage.Slot(slotCommunityPool))
}

func (s store) setCommunityPool(v *big.Int) {
	s.SetUint256(solstorage.Slot(slotCommunityPool), v)
}

func outstandingSlot(val common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.Slot(slotValidatorOutstandingRewards), val)
}

// outstandingRewards returns validator val's rewards in wei not yet withdrawn,
// commission included.
func (s store) outstandingRewards(val common.Address) *big.Int {
	return s.Uint256(outstandingSlot(val))
}

func (s store) setOutstandingRewards(val common.Address, v *big.Int) {
	s.SetUint256(outstandingSlot(val), v)
}

func (s store) deleteOutstandingRewards(val common.Address) {
	s.SetWord(outstandingSlot(val), common.Hash{})
}

func withdrawAddrSlot(delegator common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.Slot(slotDelegatorWithdrawAddr), delegator)
}

// withdrawAddress returns the account delegator's rewards are paid to: the
// delegator itself unless it has set another.
func (s store) withdrawAddress(delegator common.Address) common.Address {
	if payee := s.AddressAt(withdrawAddrSlot(delegator), 0); payee != (common.Address{}) {
		return payee
	}
	return delegator
}

// setWithdrawAddress sets the account delegator's rewards are paid to. Setting
// the delegator itself clears the record.
func (s store) setWithdrawAddress(delegator, payee common.Address) {
	if payee == delegator {
		payee = common.Address{}
	}
	s.SetAddressAt(withdrawAddrSlot(delegator), 0, payee)
}

func startingInfoBase(val, delegator common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.MapAddress(solstorage.Slot(slotDelegatorStartingInfo), val), delegator)
}

// startingInfo returns the starting info of delegator's delegation to val, or
// zero values when it has none.
func (s store) startingInfo(val, delegator common.Address) startingInfo {
	base := startingInfoBase(val, delegator)
	return startingInfo{
		previousPeriod: s.Uint64(at(base, startingInfoPreviousPeriod), startingInfoPreviousPeriod.offset),
		height:         s.Uint64(at(base, startingInfoHeight), startingInfoHeight.offset),
		stake:          s.Uint256(at(base, startingInfoStake)),
	}
}

func (s store) setStartingInfo(val, delegator common.Address, info startingInfo) {
	base := startingInfoBase(val, delegator)
	s.SetUint64(at(base, startingInfoPreviousPeriod), startingInfoPreviousPeriod.offset, info.previousPeriod)
	s.SetUint64(at(base, startingInfoHeight), startingInfoHeight.offset, info.height)
	s.SetUint256(at(base, startingInfoStake), info.stake)
}

func (s store) deleteStartingInfo(val, delegator common.Address) {
	s.clear(startingInfoBase(val, delegator), startingInfoSlots)
}

func historicalBase(val common.Address, period uint64) common.Hash {
	return solstorage.MapUint64(solstorage.MapAddress(solstorage.Slot(slotValidatorHistoricalRewards), val), period)
}

// historicalRewards returns val's historical rewards for period, or zero
// values when it has none.
func (s store) historicalRewards(val common.Address, period uint64) historicalRewards {
	base := historicalBase(val, period)
	return historicalRewards{
		cumulativeRewardRatio: s.Uint256(at(base, historicalCumulativeRewardRatio)),
		referenceCount:        uint32(s.Uint64(at(base, historicalReferenceCount), historicalReferenceCount.offset)), //nolint:gosec // the slot holds a uint32
	}
}

func (s store) setHistoricalRewards(val common.Address, period uint64, h historicalRewards) {
	base := historicalBase(val, period)
	s.SetUint256(at(base, historicalCumulativeRewardRatio), h.cumulativeRewardRatio)
	s.SetUint64(at(base, historicalReferenceCount), historicalReferenceCount.offset, uint64(h.referenceCount))
}

func (s store) deleteHistoricalRewards(val common.Address, period uint64) {
	s.clear(historicalBase(val, period), historicalRewardsSlots)
}

func currentRewardsBase(val common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.Slot(slotValidatorCurrentRewards), val)
}

// currentRewards returns val's current rewards, or zero values when it has
// none.
func (s store) currentRewards(val common.Address) currentRewards {
	base := currentRewardsBase(val)
	return currentRewards{
		rewards: s.Uint256(at(base, currentRewardsRewards)),
		period:  s.Uint64(at(base, currentRewardsPeriod), currentRewardsPeriod.offset),
	}
}

func (s store) setCurrentRewards(val common.Address, c currentRewards) {
	base := currentRewardsBase(val)
	s.SetUint256(at(base, currentRewardsRewards), c.rewards)
	s.SetUint64(at(base, currentRewardsPeriod), currentRewardsPeriod.offset, c.period)
}

func (s store) deleteCurrentRewards(val common.Address) {
	s.clear(currentRewardsBase(val), currentRewardsSlots)
}

func commissionSlot(val common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.Slot(slotValidatorAccumulatedCommission), val)
}

// accumulatedCommission returns val's commission in wei not yet withdrawn.
func (s store) accumulatedCommission(val common.Address) *big.Int {
	return s.Uint256(commissionSlot(val))
}

func (s store) setAccumulatedCommission(val common.Address, v *big.Int) {
	s.SetUint256(commissionSlot(val), v)
}

func (s store) deleteAccumulatedCommission(val common.Address) {
	s.SetWord(commissionSlot(val), common.Hash{})
}

func slashEventsBase(val common.Address) common.Hash {
	return solstorage.MapAddress(solstorage.Slot(slotValidatorSlashEvents), val)
}

func slashEventBase(val common.Address, i uint64) common.Hash {
	return solstorage.Offset(solstorage.ArrayData(slashEventsBase(val)), i*slashEventSlots)
}

// slashEventCount returns how many slash events val has stored.
func (s store) slashEventCount(val common.Address) (uint64, error) {
	n := s.Uint256(slashEventsBase(val))
	if !n.IsUint64() || n.Uint64() > maxSlashEvents {
		return 0, errInvalidStoredSlashEvents
	}
	return n.Uint64(), nil
}

// slashEvent returns val's slash event at index i, which must be below
// slashEventCount. Events are stored in ascending height.
func (s store) slashEvent(val common.Address, i uint64) slashEvent {
	base := slashEventBase(val, i)
	return slashEvent{
		height:          s.Uint64(at(base, slashEventHeight), slashEventHeight.offset),
		validatorPeriod: s.Uint64(at(base, slashEventValidatorPeriod), slashEventValidatorPeriod.offset),
		fraction:        s.Uint256(at(base, slashEventFraction)),
	}
}

// appendSlashEvent stores e after val's existing slash events.
func (s store) appendSlashEvent(val common.Address, e slashEvent) error {
	n, err := s.slashEventCount(val)
	if err != nil {
		return err
	}
	if n == maxSlashEvents {
		return errInvalidStoredSlashEvents
	}
	base := slashEventBase(val, n)
	s.SetUint64(at(base, slashEventHeight), slashEventHeight.offset, e.height)
	s.SetUint64(at(base, slashEventValidatorPeriod), slashEventValidatorPeriod.offset, e.validatorPeriod)
	s.SetUint256(at(base, slashEventFraction), e.fraction)
	s.SetUint256(slashEventsBase(val), new(big.Int).SetUint64(n+1))
	return nil
}

// deleteSlashEvents zeroes every slash event of val and the list's length.
func (s store) deleteSlashEvents(val common.Address) error {
	n, err := s.slashEventCount(val)
	if err != nil {
		return err
	}
	for i := uint64(0); i < n; i++ {
		s.clear(slashEventBase(val, i), slashEventSlots)
	}
	s.SetWord(slashEventsBase(val), common.Hash{})
	return nil
}

// ReleasePeriod is a DistributionStorage.ReleasePeriod: amount wei released
// evenly from StartTime until EndTime, both Unix nanoseconds.
type ReleasePeriod struct {
	StartTime uint64
	EndTime   uint64
	Amount    *big.Int
}

// mintState is a DistributionStorage.Mint: the end of the release period
// minting is in, zero when none is copied in, the time minted up to, and the
// wei of the period not yet minted.
type mintState struct {
	periodEnd    uint64
	lastMintTime uint64
	remaining    *big.Int
}

func releaseScheduleBase() common.Hash {
	return solstorage.Slot(slotReleaseSchedule)
}

// releaseSchedule returns the token release schedule, failing on more periods
// than maxReleasePeriods.
func (s store) releaseSchedule() ([]ReleasePeriod, error) {
	n := s.Uint64(releaseScheduleBase(), 0)
	if n > maxReleasePeriods {
		return nil, fmt.Errorf("%w: %d periods", errInvalidStoredSchedule, n)
	}
	data := solstorage.ArrayData(releaseScheduleBase())
	out := make([]ReleasePeriod, n)
	for i := range out {
		base := solstorage.Offset(data, uint64(i)*releasePeriodSlots) //nolint:gosec // i is below n
		out[i] = ReleasePeriod{
			StartTime: s.Uint64(at(base, releasePeriodStartTime), releasePeriodStartTime.offset),
			EndTime:   s.Uint64(at(base, releasePeriodEndTime), releasePeriodEndTime.offset),
			Amount:    s.Uint256(at(base, releasePeriodAmount)),
		}
	}
	return out, nil
}

// setReleaseSchedule stores periods as the token release schedule, which must
// be empty.
func (s store) setReleaseSchedule(periods []ReleasePeriod) {
	data := solstorage.ArrayData(releaseScheduleBase())
	for i, p := range periods {
		base := solstorage.Offset(data, uint64(i)*releasePeriodSlots) //nolint:gosec // i is non-negative
		s.SetUint64(at(base, releasePeriodStartTime), releasePeriodStartTime.offset, p.StartTime)
		s.SetUint64(at(base, releasePeriodEndTime), releasePeriodEndTime.offset, p.EndTime)
		s.SetUint256(at(base, releasePeriodAmount), p.Amount)
	}
	s.SetWord(releaseScheduleBase(), solstorage.Slot(uint64(len(periods))))
}

func (s store) mintState() mintState {
	base := solstorage.Slot(slotMint)
	return mintState{
		periodEnd:    s.Uint64(at(base, mintPeriodEnd), mintPeriodEnd.offset),
		lastMintTime: s.Uint64(at(base, mintLastMintTime), mintLastMintTime.offset),
		remaining:    s.Uint256(at(base, mintRemaining)),
	}
}

func (s store) setMintState(m mintState) {
	base := solstorage.Slot(slotMint)
	s.SetUint64(at(base, mintPeriodEnd), mintPeriodEnd.offset, m.periodEnd)
	s.SetUint64(at(base, mintLastMintTime), mintLastMintTime.offset, m.lastMintTime)
	s.SetUint256(at(base, mintRemaining), m.remaining)
}
