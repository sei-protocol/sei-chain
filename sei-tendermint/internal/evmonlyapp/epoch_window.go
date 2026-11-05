package evmonlyapp

import (
	"fmt"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	epochWindowModule     = "autobahn_epoch"
	epochWindowPrevKey    = "prev"
	epochWindowCurrentKey = "current"
	epochWindowNextKey    = "next"
)

func epochWindowMiscChanges(window epoch.Window) []gigatypes.MiscChange {
	changes := make([]gigatypes.MiscChange, 0, 3)
	if prev, ok := window.Prev().Get(); ok {
		changes = append(changes, epochMiscChange(epochWindowPrevKey, epoch.MarshalEpoch(prev)))
	}
	changes = append(changes,
		epochMiscChange(epochWindowCurrentKey, epoch.MarshalEpoch(window.Current())),
		epochMiscChange(epochWindowNextKey, epoch.MarshalEpoch(window.Next())),
	)
	return changes
}

func epochMiscChange(key string, value []byte) gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: epochWindowModule,
		Key:    []byte(key),
		Value:  value,
	}
}

// loadEpochWindow reads the epoch window in the committed state, or None if no
// window has been committed.
func loadEpochWindow(store *flatkv.CommitStore) (utils.Option[epoch.Window], error) {
	prevRaw, prevFound := store.Get(epochWindowModule, []byte(epochWindowPrevKey))
	currentRaw, currentFound := store.Get(epochWindowModule, []byte(epochWindowCurrentKey))
	nextRaw, nextFound := store.Get(epochWindowModule, []byte(epochWindowNextKey))
	return decodeEpochWindowRows(prevRaw, prevFound, currentRaw, currentFound, nextRaw, nextFound)
}

func decodeEpochWindowRows(
	prevRaw []byte,
	prevFound bool,
	currentRaw []byte,
	currentFound bool,
	nextRaw []byte,
	nextFound bool,
) (utils.Option[epoch.Window], error) {
	if !prevFound && !currentFound && !nextFound {
		return utils.None[epoch.Window](), nil
	}
	if !currentFound || !nextFound {
		return utils.None[epoch.Window](), fmt.Errorf("incomplete epoch window rows")
	}
	prev := utils.None[*types.Epoch]()
	if prevFound {
		prevEpoch, err := epoch.UnmarshalEpoch(prevRaw)
		if err != nil {
			return utils.None[epoch.Window](), fmt.Errorf("decode epoch window prev: %w", err)
		}
		prev = utils.Some(prevEpoch)
	}
	current, err := epoch.UnmarshalEpoch(currentRaw)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window current: %w", err)
	}
	next, err := epoch.UnmarshalEpoch(nextRaw)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window next: %w", err)
	}
	window, err := epoch.NewWindow(prev, current, next)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window: %w", err)
	}
	return utils.Some(window), nil
}
