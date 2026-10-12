package metrics

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartPprofServerServesProfiles(t *testing.T) {
	addr, err := StartPprofServer(t.Context(), "127.0.0.1:0", 0, 0)
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+addr+"/debug/pprof/goroutine?debug=1", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStartPprofServerIsOffWithoutAnAddress(t *testing.T) {
	addr, err := StartPprofServer(t.Context(), "", 0, 0)
	require.NoError(t, err)
	require.Empty(t, addr)
}

func TestStartPprofServerReportsAnAddressInUse(t *testing.T) {
	addr, err := StartPprofServer(t.Context(), "127.0.0.1:0", 0, 0)
	require.NoError(t, err)

	_, err = StartPprofServer(t.Context(), addr, 0, 0)
	require.Error(t, err)
}
