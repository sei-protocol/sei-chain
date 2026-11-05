package evmonlyapp

import (
	"fmt"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	epochWindowModule = "autobahn_epoch"
	epochWindowKey    = "window"
)

func epochWindowMiscChange(window epoch.Window) gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: epochWindowModule,
		Key:    []byte(epochWindowKey),
		Value:  epoch.MarshalWindow(window),
	}
}

// loadEpochWindow reads the epoch window in the committed state, or None if no
// window has been committed.
func loadEpochWindow(store *flatkv.CommitStore) (utils.Option[epoch.Window], error) {
	raw, found := store.Get(epochWindowModule, []byte(epochWindowKey))
	if !found {
		return utils.None[epoch.Window](), nil
	}
	window, err := epoch.WindowConv.Unmarshal(raw)
	if err != nil {
		return utils.None[epoch.Window](), fmt.Errorf("decode epoch window: %w", err)
	}
	return utils.Some(window), nil
}
