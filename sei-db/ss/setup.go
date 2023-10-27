package ss

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	seidb "github.com/sei-protocol/sei-db"
	"github.com/spf13/cast"
)

func SetupStateStore(homePath string, appOpts servertypes.AppOptions) StateStore {
	backend := cast.ToString(appOpts.Get(seidb.FlagSSBackend))
	database, err := NewStateStoreDB(homePath, BackendType(backend))
	if err != nil {
		panic(err)
	}
	return database
}
