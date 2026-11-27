package distribution

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

var (
	errNoValidatorRecords    = errors.New("validator has no distribution records")
	errNoValidator           = errors.New("no validator")
	errNoDelegation          = errors.New("no delegation")
	errNoStartingInfo        = errors.New("delegation has no starting info")
	errReferenceCount        = errors.New("historical rewards reference count out of range")
	errPeriodOrder           = errors.New("starting period is after ending period")
	errNegativeRewards       = errors.New("rewards would be negative")
	errInvalidFraction       = errors.New("rate or fraction outside zero to one")
	errOutstandingShort      = errors.New("outstanding rewards are less than what is taken from them")
	errBalanceShort          = errors.New("balance is less than the transfer")
	errNoValidatorCommission = errors.New("no validator commission to withdraw")
)

// rewardRatio returns rewards per token of tokens, 1e18-scaled and truncated.
func rewardRatio(rewards, tokens *big.Int) *big.Int {
	r := dec.FromInt(rewards)
	return r.Quo(r, tokens)
}

// rewardsBetween returns stake times the rise from ratio starting to ratio
// ending, truncated to wei.
func rewardsBetween(starting, ending, stake *big.Int) (*big.Int, error) {
	diff := new(big.Int).Sub(ending, starting)
	if diff.Sign() < 0 {
		return nil, fmt.Errorf("%w: ratio falls from %s to %s", errNegativeRewards, starting, ending)
	}
	return dec.Truncate(diff.Mul(diff, stake)), nil
}

// mulFraction returns amount times fraction, which must be from zero to one,
// truncated to wei.
func mulFraction(amount, fraction *big.Int) (*big.Int, error) {
	if fraction.Sign() < 0 || fraction.Cmp(dec.One()) > 0 {
		return nil, fmt.Errorf("%w: %s", errInvalidFraction, fraction)
	}
	return dec.Truncate(new(big.Int).Mul(amount, fraction)), nil
}

// validatorStake returns val's validator from staking.
func validatorStake(s store, val common.Address) (staking.ValidatorStake, error) {
	v, ok := staking.ReadValidatorStake(s.State, val)
	if !ok {
		return staking.ValidatorStake{}, fmt.Errorf("%w: %s", errNoValidator, val)
	}
	return v, nil
}

// openPeriod returns val's current rewards, failing when val has no records.
func (s store) openPeriod(val common.Address) (currentRewards, error) {
	cur := s.currentRewards(val)
	if cur.period == 0 {
		return currentRewards{}, fmt.Errorf("%w: %s", errNoValidatorRecords, val)
	}
	return cur, nil
}

// initializeValidator starts val's records, as x/distribution's
// initializeValidator does: period 0 holds a zero ratio, and period 1 is open.
func (s store) initializeValidator(val common.Address) {
	s.setHistoricalRewards(val, 0, historicalRewards{cumulativeRewardRatio: new(big.Int), referenceCount: 1})
	s.setCurrentRewards(val, currentRewards{rewards: new(big.Int), period: 1})
	s.setAccumulatedCommission(val, new(big.Int))
	s.setOutstandingRewards(val, new(big.Int))
}

// allocate credits tokens to val, already in the escrow, as x/distribution's
// AllocateTokensToValidator does: commission at rate, a 1e18-scaled fraction
// truncated to wei, and the rest to its delegators' open period.
func (s store) allocate(val common.Address, tokens, rate *big.Int) error {
	cur, err := s.openPeriod(val)
	if err != nil {
		return err
	}
	commission, err := mulFraction(tokens, rate)
	if err != nil {
		return err
	}
	cur.rewards = new(big.Int).Add(cur.rewards, new(big.Int).Sub(tokens, commission))
	s.setCurrentRewards(val, cur)
	s.setAccumulatedCommission(val, new(big.Int).Add(s.accumulatedCommission(val), commission))
	s.setOutstandingRewards(val, new(big.Int).Add(s.outstandingRewards(val), tokens))
	return nil
}

// incrementValidatorPeriod closes val's open period and opens the next, as
// x/distribution's IncrementValidatorPeriod does, with tokens val's stake. It
// returns the period it closed. With no tokens, the open period's rewards go
// to the DAO.
func (s store) incrementValidatorPeriod(val common.Address, tokens *big.Int) (uint64, error) {
	cur, err := s.openPeriod(val)
	if err != nil {
		return 0, err
	}
	current := new(big.Int)
	if tokens.Sign() == 0 {
		if err := s.takeOutstanding(val, cur.rewards); err != nil {
			return 0, err
		}
		if err := s.retire(cur.rewards); err != nil {
			return 0, err
		}
	} else {
		current = rewardRatio(cur.rewards, tokens)
	}
	previous := s.historicalRewards(val, cur.period-1).cumulativeRewardRatio
	if err := s.decrementReferenceCount(val, cur.period-1); err != nil {
		return 0, err
	}
	s.setHistoricalRewards(val, cur.period, historicalRewards{cumulativeRewardRatio: current.Add(current, previous), referenceCount: 1})
	s.setCurrentRewards(val, currentRewards{rewards: new(big.Int), period: cur.period + 1})
	return cur.period, nil
}

// incrementReferenceCount adds a reference to val's historical rewards for
// period.
func (s store) incrementReferenceCount(val common.Address, period uint64) error {
	h := s.historicalRewards(val, period)
	if h.referenceCount > 2 {
		return fmt.Errorf("%w: %s period %d has %d", errReferenceCount, val, period, h.referenceCount)
	}
	h.referenceCount++
	s.setHistoricalRewards(val, period, h)
	return nil
}

// decrementReferenceCount drops a reference to val's historical rewards for
// period, deleting them once none is left.
func (s store) decrementReferenceCount(val common.Address, period uint64) error {
	h := s.historicalRewards(val, period)
	if h.referenceCount == 0 {
		return fmt.Errorf("%w: %s period %d has none", errReferenceCount, val, period)
	}
	h.referenceCount--
	if h.referenceCount == 0 {
		s.deleteHistoricalRewards(val, period)
		return nil
	}
	s.setHistoricalRewards(val, period, h)
	return nil
}

// initializeDelegation starts delegator's delegation to val after val's last
// closed period, as x/distribution's initializeDelegation does, with its stake
// the tokens its shares are worth now, at height.
func (s store) initializeDelegation(height uint64, val, delegator common.Address) error {
	cur, err := s.openPeriod(val)
	if err != nil {
		return err
	}
	stake, ok := staking.DelegationTokens(s.State, delegator, val)
	if !ok {
		return fmt.Errorf("%w: %s to %s", errNoDelegation, delegator, val)
	}
	previous := cur.period - 1
	if err := s.incrementReferenceCount(val, previous); err != nil {
		return err
	}
	s.setStartingInfo(val, delegator, startingInfo{previousPeriod: previous, height: height, stake: stake})
	return nil
}

// delegationRewards returns the rewards a delegation starting at info is owed
// through endingPeriod, whose cumulative ratio is endingRatio, as
// x/distribution's calculateDelegationRewards does at height, with
// currentStake the tokens its shares are worth now. Each slash event from the
// delegation's start through height lowers the stake the periods after it pay.
func (s store) delegationRewards(val common.Address, info startingInfo, endingPeriod uint64, endingRatio *big.Int, height uint64, currentStake *big.Int) (*big.Int, error) {
	// x/distribution owes a delegation started at height nothing, since it
	// credits rewards at the start of a block. Here they are credited at the
	// end, after the transactions that may have started it, so the periods
	// alone say what it is owed.
	rewards := new(big.Int)
	startingPeriod, stake := info.previousPeriod, new(big.Int).Set(info.stake)
	n, err := s.slashEventCount(val)
	if err != nil {
		return nil, err
	}
	for i := uint64(0); i < n && !s.State.Failed(); i++ {
		e := s.slashEvent(val, i)
		if e.height < info.height {
			continue
		}
		if e.height > height {
			break
		}
		if e.validatorPeriod <= startingPeriod {
			continue
		}
		between, err := rewardsBetween(s.historicalRewards(val, startingPeriod).cumulativeRewardRatio, s.historicalRewards(val, e.validatorPeriod).cumulativeRewardRatio, stake)
		if err != nil {
			return nil, err
		}
		rewards.Add(rewards, between)
		if stake, err = mulFraction(stake, new(big.Int).Sub(dec.One(), e.fraction)); err != nil {
			return nil, err
		}
		startingPeriod = e.validatorPeriod
	}
	// The stake accumulated across slashes rounds differently from the tokens
	// the shares are worth now, so it may come out above them; the delegation is
	// never paid for more than it holds.
	if stake.Cmp(currentStake) > 0 {
		stake = currentStake
	}
	if startingPeriod > endingPeriod {
		return nil, fmt.Errorf("%w: %d after %d", errPeriodOrder, startingPeriod, endingPeriod)
	}
	final, err := rewardsBetween(s.historicalRewards(val, startingPeriod).cumulativeRewardRatio, endingRatio, stake)
	if err != nil {
		return nil, err
	}
	return rewards.Add(rewards, final), nil
}

// withdrawDelegationRewards pays the rewards delegator's delegation to val is
// owed to delegator's withdraw address and deletes its starting info, as
// x/distribution's withdrawDelegationRewards does at height, and returns what
// it paid. Rewards above val's outstanding rewards are not paid.
func (s store) withdrawDelegationRewards(height uint64, val, delegator common.Address) (*big.Int, error) {
	info := s.startingInfo(val, delegator)
	// Every starting info follows a closed period, and period 0 is never
	// closed, so a zero previous period is no starting info.
	if info.previousPeriod == 0 {
		return nil, fmt.Errorf("%w: %s to %s", errNoStartingInfo, delegator, val)
	}
	v, err := validatorStake(s, val)
	if err != nil {
		return nil, err
	}
	currentStake, ok := staking.DelegationTokens(s.State, delegator, val)
	if !ok {
		return nil, fmt.Errorf("%w: %s to %s", errNoDelegation, delegator, val)
	}
	endingPeriod, err := s.incrementValidatorPeriod(val, v.Tokens)
	if err != nil {
		return nil, err
	}
	rewards, err := s.delegationRewards(val, info, endingPeriod, s.historicalRewards(val, endingPeriod).cumulativeRewardRatio, height, currentStake)
	if err != nil {
		return nil, err
	}
	if outstanding := s.outstandingRewards(val); rewards.Cmp(outstanding) > 0 {
		rewards = outstanding
	}
	if err := s.takeOutstanding(val, rewards); err != nil {
		return nil, err
	}
	if err := s.pay(s.withdrawAddress(delegator), rewards); err != nil {
		return nil, err
	}
	if err := s.decrementReferenceCount(val, info.previousPeriod); err != nil {
		return nil, err
	}
	s.deleteStartingInfo(val, delegator)
	return rewards, nil
}

// pendingRewards returns the rewards delegator's delegation to val is owed, as
// withdrawDelegationRewards would pay them at height, without writing.
func (s store) pendingRewards(height uint64, val, delegator common.Address) (*big.Int, error) {
	info := s.startingInfo(val, delegator)
	if info.previousPeriod == 0 {
		return nil, fmt.Errorf("%w: %s to %s", errNoStartingInfo, delegator, val)
	}
	v, err := validatorStake(s, val)
	if err != nil {
		return nil, err
	}
	currentStake, ok := staking.DelegationTokens(s.State, delegator, val)
	if !ok {
		return nil, fmt.Errorf("%w: %s to %s", errNoDelegation, delegator, val)
	}
	cur, err := s.openPeriod(val)
	if err != nil {
		return nil, err
	}
	endingRatio := new(big.Int)
	if v.Tokens.Sign() != 0 {
		endingRatio = rewardRatio(cur.rewards, v.Tokens)
	}
	endingRatio.Add(endingRatio, s.historicalRewards(val, cur.period-1).cumulativeRewardRatio)
	return s.delegationRewards(val, info, cur.period, endingRatio, height, currentStake)
}

// withdrawValidatorCommission pays val's commission to its operator's withdraw
// address, as x/distribution's WithdrawValidatorCommission does, and returns
// what it paid.
func (s store) withdrawValidatorCommission(val common.Address) (*big.Int, error) {
	commission := s.accumulatedCommission(val)
	if commission.Sign() == 0 {
		return nil, fmt.Errorf("%w: %s", errNoValidatorCommission, val)
	}
	if err := s.takeOutstanding(val, commission); err != nil {
		return nil, err
	}
	s.setAccumulatedCommission(val, new(big.Int))
	if err := s.pay(s.withdrawAddress(val), commission); err != nil {
		return nil, err
	}
	return commission, nil
}

// removeValidator deletes val's records, as x/distribution's
// AfterValidatorRemoved does: it pays val's commission to its operator's
// withdraw address, and its other outstanding rewards, the rounding left after
// its last delegation was paid, to the DAO.
func (s store) removeValidator(val common.Address) error {
	cur, err := s.openPeriod(val)
	if err != nil {
		return err
	}
	commission := s.accumulatedCommission(val)
	if err := s.takeOutstanding(val, commission); err != nil {
		return err
	}
	if err := s.pay(s.withdrawAddress(val), commission); err != nil {
		return err
	}
	if err := s.retire(s.outstandingRewards(val)); err != nil {
		return err
	}
	// Once val has no delegations, the historical rewards left are the last
	// closed period's and those slash events refer to.
	n, err := s.slashEventCount(val)
	if err != nil {
		return err
	}
	for i := uint64(0); i < n && !s.State.Failed(); i++ {
		s.deleteHistoricalRewards(val, s.slashEvent(val, i).validatorPeriod)
	}
	if err := s.deleteSlashEvents(val); err != nil {
		return err
	}
	s.deleteHistoricalRewards(val, cur.period-1)
	s.deleteCurrentRewards(val)
	s.deleteAccumulatedCommission(val)
	s.deleteOutstandingRewards(val)
	return nil
}

// takeOutstanding lowers val's outstanding rewards by amount.
func (s store) takeOutstanding(val common.Address, amount *big.Int) error {
	outstanding := s.outstandingRewards(val)
	if outstanding.Cmp(amount) < 0 {
		return fmt.Errorf("%w: %s holds %s, taking %s", errOutstandingShort, val, outstanding, amount)
	}
	s.setOutstandingRewards(val, outstanding.Sub(outstanding, amount))
	return nil
}

// pay moves amount from the escrow to to.
func (s store) pay(to common.Address, amount *big.Int) error {
	return s.transfer(DistributionEscrow, to, amount)
}

// transfer moves amount from from to to.
func (s store) transfer(from, to common.Address, amount *big.Int) error {
	if amount.Sign() == 0 {
		return nil
	}
	// A balance change that fails marks the state failed for the rest of the
	// block, so the balance is checked first.
	if held := s.State.GetBalance(from); held.Cmp(amount) < 0 {
		return fmt.Errorf("%w: %s holds %s, sending %s", errBalanceShort, from, held, amount)
	}
	if err := s.State.SubBalance(from, amount); err != nil {
		return err
	}
	s.State.AddBalance(to, amount)
	return nil
}

// retire moves amount from the escrow to the DAO, where x/distribution adds it
// to the community pool. Before distribution's params are written there is no
// DAO, and amount stays in the escrow, counted in the community pool.
func (s store) retire(amount *big.Int) error {
	dao := s.epochDAO()
	if dao == (common.Address{}) {
		dao = s.params().DAOAddress
	}
	if dao == (common.Address{}) {
		s.setCommunityPool(new(big.Int).Add(s.communityPool(), amount))
		return nil
	}
	return s.pay(dao, amount)
}
