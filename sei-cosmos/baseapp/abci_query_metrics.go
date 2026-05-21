package baseapp

import storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"

// storeByNameLookup is implemented by CommitMultiStore backends (rootmulti, storev2).
type storeByNameLookup interface {
	GetStoreByName(name string) storetypes.Store
}

// abciStoreQuerySubpaths are the only subpath segments used by mounted store
// Query implementations (/key, /subspace). See store/rootmulti and storev2/commitment.
var abciStoreQuerySubpaths = map[string]struct{}{
	"key":      {},
	"subspace": {},
}

// abciQueryMetricRoute returns a bounded label for ABCI query metrics.
// Raw client paths are not used directly to avoid unbounded metric cardinality.
//
// Rules:
//   - Registered gRPC query paths are returned as-is (finite set at startup).
//   - Legacy paths use a fixed prefix + registered segment shape.
//   - Everything else is "other".
func (app *BaseApp) abciQueryMetricRoute(reqPath string) string {
	if app.grpcQueryRouter != nil && app.grpcQueryRouter.Route(reqPath) != nil {
		return reqPath
	}

	parts := splitPath(reqPath)
	if len(parts) == 0 {
		return "other"
	}

	switch parts[0] {
	case "app":
		if len(parts) >= 2 {
			switch parts[1] {
			case "simulate", "version", "snapshots":
				return "app/" + parts[1]
			}
		}
		return "app/unknown"

	case "store":
		return app.abciStoreQueryMetricRoute(parts)

	case "custom":
		if len(parts) >= 2 && parts[1] != "" && app.queryRouter != nil && app.queryRouter.Route(parts[1]) != nil {
			return "custom/" + parts[1]
		}
		return "custom/unknown"

	default:
		return "other"
	}
}

func (app *BaseApp) abciStoreQueryMetricRoute(parts []string) string {
	unknownRoute := "store/unknown"
	if len(parts) < 3 {
		return unknownRoute
	}
	storeName, subpath := parts[1], parts[2]
	if _, ok := abciStoreQuerySubpaths[subpath]; !ok {
		return unknownRoute
	}
	if !app.storeRegisteredForQuery(storeName) {
		return unknownRoute
	}
	return "store/" + storeName + "/" + subpath
}

func (app *BaseApp) storeRegisteredForQuery(name string) bool {
	if lookup, ok := app.cms.(storeByNameLookup); ok && lookup.GetStoreByName(name) != nil {
		return true
	}
	if app.qms != nil {
		if lookup, ok := app.qms.(storeByNameLookup); ok && lookup.GetStoreByName(name) != nil {
			return true
		}
	}
	return false
}

// abciQueryMetricRouteLabel is kept as "path" for compatibility with existing dashboards.
const abciQueryMetricRouteLabel = "path"
