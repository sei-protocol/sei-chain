package server

import (
	"fmt"
	"strings"

	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

// GetPruningOptionsFromFlags parses command flags and returns the correct
// PruningOptions. If a pruning strategy is provided, that will be parsed and
// returned, otherwise, it is assumed custom pruning options are provided.
func GetPruningOptionsFromFlags(appOpts types.AppOptions) (storetypes.PruningOptions, error) {
	// New format (iavl.*) takes priority, fallback to legacy top-level keys for backward compatibility
	strategy := cast.ToString(appOpts.Get(FlagIAVLPruning))
	if strategy == "" {
		strategy = cast.ToString(appOpts.Get(FlagPruning))
	}
	strategy = strings.ToLower(strategy)

	switch strategy {
	case storetypes.PruningOptionDefault, storetypes.PruningOptionNothing, storetypes.PruningOptionEverything:
		return storetypes.NewPruningOptionsFromString(strategy), nil

	case storetypes.PruningOptionCustom:
		keepRecent := appOpts.Get(FlagIAVLPruningKeepRecent)
		if keepRecent == nil {
			keepRecent = appOpts.Get(FlagPruningKeepRecent)
		}
		keepEvery := appOpts.Get(FlagIAVLPruningKeepEvery)
		if keepEvery == nil {
			keepEvery = appOpts.Get(FlagPruningKeepEvery)
		}
		interval := appOpts.Get(FlagIAVLPruningInterval)
		if interval == nil {
			interval = appOpts.Get(FlagPruningInterval)
		}

		opts := storetypes.NewPruningOptions(
			cast.ToUint64(keepRecent),
			cast.ToUint64(keepEvery),
			cast.ToUint64(interval),
		)

		if err := opts.Validate(); err != nil {
			return opts, fmt.Errorf("invalid custom pruning options: %w", err)
		}

		return opts, nil

	default:
		return store.PruningOptions{}, fmt.Errorf("unknown pruning strategy %s", strategy)
	}
}
