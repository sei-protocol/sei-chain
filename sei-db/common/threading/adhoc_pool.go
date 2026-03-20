package threading

import (
	"context"
	"fmt"
)

var _ Pool = (*adHocPool)(nil)

// adHocPool is a Pool that runs each task in a new goroutine.
// Intended for use in unit tests or where performance is not important.
type adHocPool struct{}

// NewAdHocPool creates a Pool that runs each submitted task in a one-off goroutine.
func NewAdHocPool() Pool {
	return &adHocPool{}
}

func (p *adHocPool) Submit(ctx context.Context, task func()) error {
	if task == nil {
		return fmt.Errorf("adhoc pool: nil task")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	go task()
	return nil
}
