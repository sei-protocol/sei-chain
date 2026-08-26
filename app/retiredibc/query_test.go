package retiredibc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryResponse(t *testing.T) {
	for _, path := range []string{
		"/store/ibc/key",
		"/store/ibc/subspace",
		"/store/transfer/key",
		"/store/transfer/subspace",
		"/store/capability/key",
		"/store/capability/subspace",
	} {
		t.Run(path, func(t *testing.T) {
			response := QueryResponse(path)
			require.NotNil(t, response)
			require.Equal(t, "ibc", response.Codespace)
			require.Equal(t, uint32(103), response.Code)
			require.Equal(t, "ibc module is deprecated", response.Log)
		})
	}

	for _, path := range []string{
		"/store/bank/key",
		"/store/ibc-transfer/key",
		"/custom/ibc/query",
		"/store/ibc/unknown",
	} {
		t.Run(path, func(t *testing.T) {
			require.Nil(t, QueryResponse(path))
		})
	}
}
