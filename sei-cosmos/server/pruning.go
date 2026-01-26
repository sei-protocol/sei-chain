package server

import (
	"fmt"
	"strings"

	"github.com/spf13/cast"

	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
)

// New config keys under [iavl] section (v6.3.0+).
// Legacy top-level keys (FlagPruning, etc.) are kept as fallback for backward compatibility.
// TODO: Remove legacy fallback once all nodes have migrated to v6.3.0+
const (
	iavlPruningKey           = "iavl.pruning"
	iavlPruningKeepRecentKey = "iavl.pruning-keep-recent"
	iavlPruningKeepEveryKey  = "iavl.pruning-keep-every"
	iavlPruningIntervalKey   = "iavl.pruning-interval"
)

// GetPruningOptionsFromFlags parses command flags and returns the correct
// PruningOptions. If a pruning strategy is provided, that will be parsed and
// returned, otherwise, it is assumed custom pruning options are provided.
func GetPruningOptionsFromFlags(appOpts types.AppOptions) (storetypes.PruningOptions, error) {
	// New format (iavl.*) takes priority, fallback to legacy top-level keys for backward compatibility
	strategy := strings.ToLower(getStringOption(appOpts, iavlPruningKey, FlagPruning))

	switch strategy {
	case storetypes.PruningOptionDefault, storetypes.PruningOptionNothing, storetypes.PruningOptionEverything:
		return storetypes.NewPruningOptionsFromString(strategy), nil

	case storetypes.PruningOptionCustom:
		opts := storetypes.NewPruningOptions(
			getUint64Option(appOpts, iavlPruningKeepRecentKey, FlagPruningKeepRecent),
			getUint64Option(appOpts, iavlPruningKeepEveryKey, FlagPruningKeepEvery),
			getUint64Option(appOpts, iavlPruningIntervalKey, FlagPruningInterval),
		)

		if err := opts.Validate(); err != nil {
			return opts, fmt.Errorf("invalid custom pruning options: %w", err)
		}

		return opts, nil

	default:
		return store.PruningOptions{}, fmt.Errorf("unknown pruning strategy %s", strategy)
	}
}

func getStringOption(appOpts types.AppOptions, primary, fallback string) string {
	primaryValue := appOpts.Get(primary)
	if primaryValue == nil {
		return cast.ToString(appOpts.Get(fallback))
	}

	value := cast.ToString(primaryValue)
	if value == "" {
		return cast.ToString(appOpts.Get(fallback))
	}

	return value
}

func getUint64Option(appOpts types.AppOptions, primary, fallback string) uint64 {
	primaryValue := appOpts.Get(primary)
	if primaryValue == nil {
		return cast.ToUint64(appOpts.Get(fallback))
	}

	return cast.ToUint64(primaryValue)
}
