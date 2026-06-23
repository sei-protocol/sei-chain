package hashlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoOpHashLogger(t *testing.T) {
	l := NewNoOpHashLogger()
	require.NotPanics(t, func() {
		l.ReportChangeset(1, nil)
	})
	require.NoError(t, l.ReportHash(1, "anything", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "", nil))
	require.NoError(t, l.Close())
}
