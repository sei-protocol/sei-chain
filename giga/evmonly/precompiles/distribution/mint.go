package distribution

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// maxReleasePeriods is the most periods a token release schedule may hold.
const maxReleasePeriods = 64

var (
	errInvalidSchedule       = errors.New("invalid token release schedule")
	errScheduleWritten       = errors.New("the token release schedule is already written")
	errInvalidStoredSchedule = errors.New("invalid stored token release schedule")
)

// WriteReleaseSchedule writes schedule as the token release schedule, for
// genesis, with minting starting at now: a period that has ended by now
// releases nothing, and one in progress releases its whole amount over what is
// left of it. It fails when a schedule is already written or schedule is
// invalid.
func WriteReleaseSchedule(state precompiles.State, schedule []ReleasePeriod, now uint64) error {
	s := newStore(state)
	if s.Uint64(releaseScheduleBase(), 0) != 0 {
		return errScheduleWritten
	}
	if err := checkSchedule(schedule); err != nil {
		return err
	}
	s.setReleaseSchedule(schedule)
	s.setMintState(mintState{lastMintTime: now, remaining: new(big.Int)})
	return nil
}

// checkSchedule requires at most maxReleasePeriods periods, in start order,
// each ending after it starts and none overlapping, with non-negative amounts.
func checkSchedule(schedule []ReleasePeriod) error {
	if len(schedule) > maxReleasePeriods {
		return fmt.Errorf("%w: %d periods, at most %d", errInvalidSchedule, len(schedule), maxReleasePeriods)
	}
	for i, p := range schedule {
		switch {
		case p.EndTime <= p.StartTime:
			return fmt.Errorf("%w: period %d ends at or before it starts", errInvalidSchedule, i)
		case i > 0 && p.StartTime < schedule[i-1].EndTime:
			return fmt.Errorf("%w: period %d starts before period %d ends", errInvalidSchedule, i, i-1)
		case p.Amount == nil || p.Amount.Sign() < 0 || p.Amount.BitLen() > 256:
			return fmt.Errorf("%w: period %d's amount does not fit uint256", errInvalidSchedule, i)
		}
	}
	return nil
}

// mint returns the wei the token release schedule releases from the last mint
// until t, and records them as minted. Gaps between periods release nothing.
func (s store) mint(t uint64) (*big.Int, error) {
	schedule, err := s.releaseSchedule()
	if err != nil {
		return nil, err
	}
	m := s.mintState()
	minted := new(big.Int)
	// Each pass but the last finishes a period, so the schedule bounds them.
	for range len(schedule) + 1 {
		if m.periodEnd == 0 {
			next, ok := nextReleasePeriod(schedule, m.lastMintTime)
			if !ok {
				break
			}
			m.periodEnd, m.remaining = next.EndTime, new(big.Int).Set(next.Amount)
			m.lastMintTime = max(m.lastMintTime, next.StartTime)
		}
		if t <= m.lastMintTime {
			break
		}
		// A period t has passed releases all it has left, and minting goes on
		// from the next period's start.
		if t >= m.periodEnd {
			minted.Add(minted, m.remaining)
			m = mintState{lastMintTime: m.periodEnd, remaining: new(big.Int)}
			continue
		}
		// The share of what is left that t covers of the time left, truncated;
		// the truncation is minted later in the period.
		share := new(big.Int).Mul(m.remaining, new(big.Int).SetUint64(t-m.lastMintTime))
		share.Quo(share, new(big.Int).SetUint64(m.periodEnd-m.lastMintTime))
		minted.Add(minted, share)
		m.remaining = new(big.Int).Sub(m.remaining, share)
		m.lastMintTime = t
		break
	}
	s.setMintState(m)
	return minted, nil
}

// nextReleasePeriod returns the first period of schedule that ends after
// after.
func nextReleasePeriod(schedule []ReleasePeriod, after uint64) (ReleasePeriod, bool) {
	for _, p := range schedule {
		if p.EndTime > after {
			return p, true
		}
	}
	return ReleasePeriod{}, false
}
