package baseapp

import (
	"context"
	"fmt"
	"net"
	"strings"

	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
	"github.com/spf13/cast"
	"google.golang.org/grpc/peer"
)

const (
	FlagQueryDisableLimits = "query.disable-limits"
	FlagQueryTrustedCIDRs  = "query.trusted-cidrs"
	FlagQueryMaxLimit      = "query.max-limit"
	FlagQueryMaxOffset     = "query.max-offset"
	FlagQueryMaxIterations = "query.max-iterations"
)

type trustedCIDRMatcher struct {
	networks []*net.IPNet
}

func newTrustedCIDRMatcher(cidrs []string) *trustedCIDRMatcher {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		networks = append(networks, network)
	}
	return &trustedCIDRMatcher{networks: networks}
}

func (m *trustedCIDRMatcher) contains(ipStr string) bool {
	if m == nil {
		return false
	}
	ip := net.ParseIP(stripHostPort(ipStr))
	if ip == nil {
		return false
	}
	for _, network := range m.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func readQueryConfig(appOpts servertypes.AppOptions) (srvconfig.QueryConfig, error) {
	cfg := srvconfig.DefaultQueryConfig()
	var err error

	if v := appOpts.Get(FlagQueryDisableLimits); v != nil {
		if cfg.DisableLimits, err = cast.ToBoolE(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryDisableLimits, err)
		}
	}
	if v := appOpts.Get(FlagQueryTrustedCIDRs); v != nil {
		if cfg.TrustedCIDRs, err = cast.ToStringSliceE(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryTrustedCIDRs, err)
		}
	}
	if v := appOpts.Get(FlagQueryMaxLimit); v != nil {
		if cfg.MaxLimit, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryMaxLimit, err)
		}
	}
	if v := appOpts.Get(FlagQueryMaxOffset); v != nil {
		if cfg.MaxOffset, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryMaxOffset, err)
		}
	}
	if v := appOpts.Get(FlagQueryMaxIterations); v != nil {
		if cfg.MaxIterations, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryMaxIterations, err)
		}
	}
	return cfg, nil
}

func warnQueryConfig(cfg srvconfig.QueryConfig) {
	for _, warning := range srvconfig.ValidateQueryConfig(cfg) {
		logger.Warn(warning)
	}
}

func (app *BaseApp) enrichABCIQueryContext(ctx context.Context, sdkCtx sdk.Context) sdk.Context {
	sdkCtx = sdkCtx.WithIsABCIQuery(true)

	if app.queryConfig.DisableLimits {
		return sdkCtx.WithPaginationLimits(sdk.NoPaginationLimits())
	}

	originIP := queryOriginIP(ctx)
	trusted := app.trustedOriginMatcher != nil && app.trustedOriginMatcher.contains(originIP)
	if trusted {
		logger.Debug("query pagination limits disabled for trusted origin", "origin", originIP)
		return sdkCtx.WithPaginationLimits(sdk.NoPaginationLimits())
	}

	return sdkCtx.WithPaginationLimits(sdk.UntrustedPaginationLimits(
		app.queryConfig.MaxLimit,
		app.queryConfig.MaxOffset,
		app.queryConfig.MaxIterations,
	))
}

func queryOriginIP(ctx context.Context) string {
	if callInfo := rpctypes.GetCallInfo(ctx); callInfo != nil {
		if addr := callInfo.RemoteAddr(); addr != "" {
			return addr
		}
	}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}

func stripHostPort(addr string) string {
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "[") {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return strings.Trim(addr, "[]")
		}
		return host
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
