package rpc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
)

func TestHandler_InvalidTrustedProxyCIDRsDoesNotPanic(t *testing.T) {
	cfg := config.DefaultRPCConfig()
	cfg.RateLimitingEnabled = true
	cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}

	require.NotPanics(t, func() {
		h := Handler(cfg, core.RoutesMap{})
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	})
}
