package metrics

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupPrometheusIsIdempotent(t *testing.T) {
	require.NoError(t, SetupPrometheus())
	require.NoError(t, SetupPrometheus())
	require.NotNil(t, MainLoop())
}

func TestSetPhaseIsSafeForConcurrentCallers(t *testing.T) {
	require.NoError(t, SetupPrometheus())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SetPhase(PhaseConsensus)
			SetPhase(PhaseExecution)
			SetPhase(PhaseStorage)
		}()
	}
	wg.Wait()
}
