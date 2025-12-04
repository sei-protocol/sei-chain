package consensus

import (
	"context"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

// TimeoutTicker is a timer that schedules timeouts
// conditional on the height/round/step in the timeoutInfo.
// The timeoutInfo.Duration may be non-positive.
type TimeoutTicker interface {
	Run(context.Context) error
	Chan() <-chan timeoutInfo       // on which to receive a timeout
	ScheduleTimeout(ti timeoutInfo) // reset the timer
}

// timeoutTicker wraps time.Timer,
// scheduling timeouts only for greater height/round/step
// than what it's already seen.
// Timeouts are scheduled along the tickChan,
// and fired on the tockChan.
type timeoutTicker struct {
	logger   log.Logger
	tick     utils.Mutex[*utils.AtomicSend[utils.Option[timeoutInfo]]] // for scheduling timeouts
	tockChan chan timeoutInfo                                          // for notifying about them
}

// NewTimeoutTicker returns a new TimeoutTicker.
func NewTimeoutTicker(logger log.Logger) TimeoutTicker {
	tt := &timeoutTicker{
		logger:   logger,
		tick:     utils.NewMutex(utils.Alloc(utils.NewAtomicSend(utils.None[timeoutInfo]()))),
		tockChan: make(chan timeoutInfo),
	}
	return tt
}

// Chan returns a channel on which timeouts are sent.
func (t *timeoutTicker) Chan() <-chan timeoutInfo {
	return t.tockChan
}

// ScheduleTimeout schedules a new timeout, which replaces the previous one.
// Noop if a timeout for a later height/round/step has been already scheduled.
func (t *timeoutTicker) ScheduleTimeout(newti timeoutInfo) {
	for tick := range t.tick.Lock() {
		old := tick.Load()
		if oldti, ok := old.Get(); !ok || oldti.Less(&newti) {
			tick.Store(utils.Some(newti))
		}
	}
}

func (t *timeoutTicker) tickSubscribe() utils.AtomicRecv[utils.Option[timeoutInfo]] {
	for tick := range t.tick.Lock() {
		return tick.Subscribe()
	}
	panic("unreachable")
}

// timers are interupted and replaced by new ticks from later steps
// timeouts of 0 on the tickChan will be immediately relayed to the tockChan
func (t *timeoutTicker) Run(ctx context.Context) error {
	tockSend := utils.NewAtomicSend(utils.None[timeoutInfo]()) // last fired timeout
	tockRecv := tockSend.Subscribe()
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			// Task measuring timeouts. Owns tockSend.
			return t.tickSubscribe().Iter(ctx, func(ctx context.Context, mti utils.Option[timeoutInfo]) error {
				ti, ok := mti.Get()
				if !ok {
					return nil
				}
				t.logger.Debug("Internal state machine timeout scheduled", "duration", ti.Duration, "height", ti.Height, "round", ti.Round, "step", ti.Step)
				if err := utils.Sleep(ctx, ti.Duration); err != nil {
					return err
				}
				t.logger.Debug("Internal state machine timeout elapsed ", "duration", ti.Duration, "height", ti.Height, "round", ti.Round, "step", ti.Step)
				tockSend.Store(utils.Some(ti))
				return nil
			})
		})
		// Task reporting timeouts via channel. Owns tockRecv.
		// TODO(gprusak): it would be better to expose t.tock directly,
		// however the receiving task doesn't support receiving from AtomicWatch yet.
		return tockRecv.Iter(ctx, func(ctx context.Context, mto utils.Option[timeoutInfo]) error {
			to, ok := mto.Get()
			if !ok {
				return nil
			}
			return utils.Send(ctx, t.tockChan, to)
		})
	})
}
