package consensus

import (
	"context"
	"fmt"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"testing"
	"time"
)

func TestTicker(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		logger, _ := log.NewDefaultLogger("plain", "debug")
		ticker := NewTimeoutTicker(logger)
		ch := ticker.Chan()
		s.SpawnBg(func() error {
			err := ticker.Run(ctx)
			if ctx.Err() == nil {
				return fmt.Errorf("ticker terminated with %v, before the test ended", err)
			}
			return utils.IgnoreCancel(err)
		})
		if got := len(ch); got > 0 {
			return fmt.Errorf("expected empty, got len=%v", got)
		}

		t.Log("Fill the channel.")
		h := int64(0)
		for h < int64(cap(ch)) {
			h += 1
			ticker.ScheduleTimeout(timeoutInfo{Height: h, Duration: 0})
			for len(ch) < int(h) {
				if err := utils.Sleep(ctx, 10*time.Millisecond); err != nil {
					return err
				}
			}
		}
		t.Log("Add a bunch of timeouts blindly.")
		for range 3 {
			h += 1
			ticker.ScheduleTimeout(timeoutInfo{Height: h, Duration: 0})
			if err := utils.Sleep(ctx, 100*time.Millisecond); err != nil {
				return err
			}
		}
		t.Log("Await the latest timeout")
		for {
			got, err := utils.Recv(ctx, ch)
			if err != nil {
				return err
			}
			if got.Height == h {
				return nil
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
