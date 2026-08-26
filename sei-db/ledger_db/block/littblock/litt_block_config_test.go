package littblock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateRetentionTime(t *testing.T) {
	cfg, err := DefaultConfig(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	// RetentionTime gates reclamation underneath the watermark, so a non-positive value would
	// release records the moment the watermark passed them, removing the failsafe entirely.
	for _, retentionTime := range []time.Duration{0, -time.Second} {
		cfg.RetentionTime = retentionTime
		require.ErrorContains(t, cfg.Validate(), "RetentionTime")
	}
}
