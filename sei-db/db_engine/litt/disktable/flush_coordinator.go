package disktable

import (
	"fmt"
	"time"

	"github.com/Layr-Labs/eigenda/common/structures"
	"github.com/Layr-Labs/eigenda/litt/util"
	"golang.org/x/time/rate"
)

// Size of the request channel buffer. This should be large enough to handle bursts of flush requests without
// blocking the caller, but not so large that it wastes memory.
const requestChanBufferSize = 128

// Used to make very rapid flushes more efficient. Essentially batches multiple flushes into individual flushes.
// If configured to only allow one flush per X milliseconds and multiple flushes are requested during that time period,
// will only perform one flush at the end of the time period. Does not change the semantics of flush from the
// caller's perspective, just the performance/timing.
type flushCoordinator struct {
	// Used to manage the lifecycle of LittDB threading resources.
	errorMonitor *util.ErrorMonitor

	// The function that actually performs the flush on the underlying database.
	internalFlush func() error

	// Channel to send flush requests to the control loop.
	requestChan chan any

	// used to rate limit flushes
	rateLimiter *rate.Limiter
}

// A request to flush the underlying database. When the flush is eventually performed, a response is sent on
// the request's channel. The response is nil if the flush was successful, or an error if it failed.
type flushCoordinatorRequest chan error

// Creates a new flush coordinator.
//
// - internalFlush: the function that actually performs the flush on the underlying database
// - flushPeriod: the minimum time period between flushes, if zero then no batching is performed
func newFlushCoordinator(
	errorMonitor *util.ErrorMonitor,
	internalFlush func() error,
	flushPeriod time.Duration,
) *flushCoordinator {

	fc := &flushCoordinator{
		errorMonitor:  errorMonitor,
		internalFlush: internalFlush,
		requestChan:   make(chan any, requestChanBufferSize),
	}

	if flushPeriod > 0 {
		fc.rateLimiter = rate.NewLimiter(rate.Every(flushPeriod), 1)
		go fc.controlLoop()
	}

	return fc
}

// Flushes the underlying database. May wait to call flush based on the configured flush period.
func (c *flushCoordinator) Flush() error {
	if c.rateLimiter == nil {
		// we can short circuit and just call the internal flush directly, flush frequency is infinitely high
		return c.internalFlush()
	}

	request := make(flushCoordinatorRequest, 1)

	// send the request
	err := util.Send(c.errorMonitor, c.requestChan, request)
	if err != nil {
		return fmt.Errorf("error sending flush coordinator request: %w", err)
	}

	// await the response
	response, err := util.Await(c.errorMonitor, request)
	if err != nil {
		return fmt.Errorf("error awaiting flush coordinator response: %w", err)
	}

	if response != nil {
		return fmt.Errorf("flush failed: %w", response)
	}
	return nil

}

// The control loop that manages flush timing.
func (c *flushCoordinator) controlLoop() {
	defer close(c.requestChan)

	// requests that are waiting for a flush to be performed
	waitingRequests := structures.NewQueue[flushCoordinatorRequest](1024)

	// timer used to wait until the next flush can be performed
	timer := time.NewTimer(0)
	defer timer.Stop()
	var timerActive bool

	for {

		if timerActive {
			// There are pending flushes we want to handle, but we need to wait until the timer expires.
			select {
			case <-c.errorMonitor.ImmediateShutdownRequired():
				return
			case request := <-c.requestChan:
				waitingRequests.Push(request.(flushCoordinatorRequest))
			case <-timer.C:

				// we can now perform a flush
				err := c.internalFlush()
				// send a response to each waiting caller
				for request, ok := waitingRequests.TryPop(); ok; request, ok = waitingRequests.TryPop() {
					request <- err
				}

				timerActive = false
			}
		} else {
			// We don't have any pending flush requests, so we aren't waiting on the timer. If we get a new request,
			// check to see if the rate limiter will allow it to be flushed immediately, and do so if possible.
			select {
			case <-c.errorMonitor.ImmediateShutdownRequired():
				return
			case request := <-c.requestChan:
				if c.rateLimiter.Allow() {
					// we can flush immediately, it's been long enough since the last flush
					request.(flushCoordinatorRequest) <- c.internalFlush()
				} else {
					// we need to wait before flushing, add the request to the queue
					waitingRequests.Push(request.(flushCoordinatorRequest))

					timeUntilPermitted := c.rateLimiter.Reserve().Delay()
					timer.Reset(timeUntilPermitted)
					timerActive = true
				}
			}
		}
	}
}
