package evmrpc

import (
	"sync"
	"testing"
	"time"
)

func TestRunWithRecoveryHandlesPanic(t *testing.T) {
	var once sync.Once
	recovered := make(chan struct{})
	SetPanicHook(func(interface{}) {
		once.Do(func() { close(recovered) })
	})
	defer SetPanicHook(nil)

	runWithRecovery(func() {
		panic("should be handled")
	})

	select {
	case <-recovered:
	case <-time.After(time.Second):
		t.Fatal("expected panic to be recovered")
	}
}
