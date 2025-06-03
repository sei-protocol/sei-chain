package sc

import (
	"os"

	rootmulti2 "github.com/cosmos/cosmos-sdk/storev2/rootmulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/tools/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/tendermint/tendermint/libs/log"
)

func NewStore(homeDir string) *rootmulti2.Store {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	// Creating CMS for store V2
	scConfig := config.DefaultStateCommitConfig()
	scConfig.Enable = true
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Enable = false
	cmsV2 := rootmulti2.NewStore(homeDir, logger, scConfig, ssConfig, false)
	for _, key := range utils.ModuleKeys {
		cmsV2.MountStoreWithDB(key, sdk.StoreTypeIAVL, nil)
	}
	err := cmsV2.LoadLatestVersion()
	if err != nil {
		panic(err)
	}
	return cmsV2
}
