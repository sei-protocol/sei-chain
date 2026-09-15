package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupPrometheusIsIdempotent(t *testing.T) {
	require.NoError(t, SetupPrometheus())
	require.NoError(t, SetupPrometheus())
	require.NotNil(t, MainLoop())
}
