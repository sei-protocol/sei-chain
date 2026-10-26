package evmonlyapp

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// loadEpochWindow reads the epoch window of the store's latest version. It is
// None if no window was committed, and an error if only part of it is stored.
func loadEpochWindow(store *flatkv.CommitStore) (utils.Option[epoch.StateWindow], error) {
	prev, foundPrev := store.Get(epoch.StateModuleName, []byte(epoch.PrevEpochRecordKey))
	current, foundCurrent := store.Get(epoch.StateModuleName, []byte(epoch.CurrentEpochRecordKey))
	next, foundNext := store.Get(epoch.StateModuleName, []byte(epoch.NextEpochRecordKey))
	if !foundPrev && !foundCurrent && !foundNext {
		return utils.None[epoch.StateWindow](), nil
	}
	if !foundPrev || !foundCurrent || !foundNext {
		return utils.None[epoch.StateWindow](), fmt.Errorf("incomplete epoch window: prev %t, current %t, next %t", foundPrev, foundCurrent, foundNext)
	}
	window, err := epoch.DecodeStateWindow(prev, current, next)
	if err != nil {
		return utils.None[epoch.StateWindow](), fmt.Errorf("decode epoch window: %w", err)
	}
	return utils.Some(window), nil
}
