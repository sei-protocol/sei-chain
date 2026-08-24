package rpc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/rpc/core"
)

func TestHandler_InvalidTrustedProxyCIDRs(t *testing.T) {
	cfg := config.DefaultRPCConfig()
	cfg.RateLimitingEnabled = true
	cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}

	h, err := Handler(cfg, core.RoutesMap{})
	require.Error(t, err)
	require.Nil(t, h)
}

func TestHandler_RateLimitingEnabled(t *testing.T) {
	cfg := config.DefaultRPCConfig()
	cfg.RateLimitingEnabled = true

	h, err := Handler(cfg, core.RoutesMap{})
	require.NoError(t, err)
	require.NotNil(t, h)
}
