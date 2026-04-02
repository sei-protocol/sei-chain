package threading

import "sync"

var _ Pool = (*adHocPool)(nil)

// adHocPool is a Pool that runs each task in a new goroutine.
// Intended for use in unit tests or where performance is not important.
type adHocPool struct {
	wg     sync.WaitGroup
	closed bool
}

// NewAdHocPool creates a Pool that runs each submitted task in a one-off goroutine.
func NewAdHocPool() Pool {
	return &adHocPool{}
}

func (p *adHocPool) Submit(task func()) {
	if task == nil {
		return
	}
	if p.closed {
		panic("threading: submit on closed pool")
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		task()
	}()
}

func (p *adHocPool) Close() {
	p.closed = true
	p.wg.Wait()
}
