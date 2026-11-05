package evmonlyapp

import (
	"errors"
	"fmt"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	epochWindowModule     = "autobahn_epoch"
	epochWindowPrevKey    = "prev"
	epochWindowCurrentKey = "current"
	epochWindowNextKey    = "next"
)

// epochWindowMiscChanges returns the writes that commit window, one row per epoch. It
// returns an error if window has no previous epoch.
func epochWindowMiscChanges(window epoch.Window) ([]gigatypes.MiscChange, error) {
	prev, ok := window.Prev().Get()
	if !ok {
		return nil, errors.New("epoch window to commit has no previous epoch")
	}
	return []gigatypes.MiscChange{
		epochMiscChange(epochWindowPrevKey, epoch.MarshalEpoch(prev)),
		epochMiscChange(epochWindowCurrentKey, epoch.MarshalEpoch(window.Current())),
		epochMiscChange(epochWindowNextKey, epoch.MarshalEpoch(window.Next())),
	}, nil
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
	row := func(key string) utils.Option[[]byte] {
		if raw, found := store.Get(epochWindowModule, []byte(key)); found {
			return utils.Some(raw)
		}
		return utils.None[[]byte]()
	}
	return decodeEpochWindowRows(row(epochWindowPrevKey), row(epochWindowCurrentKey), row(epochWindowNextKey))
}

// decodeEpochWindowRows returns the window held by the prev, current and next rows, or
// None if all three are absent. It returns an error if only some of them are present.
func decodeEpochWindowRows(prevRow, currentRow, nextRow utils.Option[[]byte]) (utils.Option[epoch.Window], error) {
	prevRaw, prevFound := prevRow.Get()
	currentRaw, currentFound := currentRow.Get()
	nextRaw, nextFound := nextRow.Get()
	if !prevFound && !currentFound && !nextFound {
		return utils.None[epoch.Window](), nil
	}
	if !prevFound || !currentFound || !nextFound {
		return utils.None[epoch.Window](), fmt.Errorf(
			"incomplete epoch window rows: prev %t, current %t, next %t", prevFound, currentFound, nextFound)
	}
	prev, err := epoch.UnmarshalEpoch(prevRaw)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window prev: %w", err)
	}
	current, err := epoch.UnmarshalEpoch(currentRaw)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window current: %w", err)
	}
	next, err := epoch.UnmarshalEpoch(nextRaw)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window next: %w", err)
	}
	window, err := epoch.NewWindow(utils.Some(prev), current, next)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window: %w", err)
	}
	return utils.Some(window), nil
}
