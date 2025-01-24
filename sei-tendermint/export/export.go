package export

import (
	"github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/internal/store"
)

var NewBlockStore = store.NewBlockStore
var NewStore = state.NewStore
