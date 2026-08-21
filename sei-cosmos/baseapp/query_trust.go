package baseapp

import (
	"context"
	"fmt"
	"net"
	"strings"

	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
	"github.com/spf13/cast"
	"google.golang.org/grpc/peer"
)

const (
	FlagQueryTrustedCIDRs     = "query.trusted-cidrs"
	FlagQueryTrustedScanLimit = "query.trusted-scan-limit"
)

type trustedCIDRMatcher struct {
	networks []*net.IPNet
}

// newTrustedCIDRMatcher returns a matcher for parseable entries in cidrs, skipping the rest.
// warnQueryConfig should run before this so skipped entries are logged.
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

	if v := appOpts.Get(FlagQueryTrustedCIDRs); v != nil {
		if cfg.TrustedCIDRs, err = cast.ToStringSliceE(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryTrustedCIDRs, err)
		}
	}
	if v := appOpts.Get(FlagQueryTrustedScanLimit); v != nil {
		if cfg.TrustedScanLimit, err = cast.ToUint64E(v); err != nil {
			return cfg, fmt.Errorf("invalid %s: %w", FlagQueryTrustedScanLimit, err)
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
	originIP := queryOriginIP(ctx)
	trusted := app.trustedOriginMatcher != nil && app.trustedOriginMatcher.contains(originIP)
	sdkCtx = sdkCtx.WithIsTrustedQueryOrigin(trusted)

	if trusted {
		if app.queryConfig.TrustedScanLimit == 0 {
			sdkCtx = sdkCtx.WithQueryScanLimit(false, 0)
		} else {
			sdkCtx = sdkCtx.WithQueryScanLimit(true, app.queryConfig.TrustedScanLimit)
		}
		logger.Debug(
			"query pagination using trusted scan limit",
			"origin", originIP,
			"limit", app.queryConfig.TrustedScanLimit,
		)
		return sdkCtx
	}

	return sdkCtx.WithQueryScanLimit(true, query.MaxScanLimit)
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
