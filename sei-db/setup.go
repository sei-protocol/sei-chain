package store

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	FlagSeiDB = "seidb.enable"
)

func SetupSeiDB(
	logger log.Logger,
	appOpts servertypes.AppOptions,
	baseAppOptions []func(*baseapp.BaseApp),
) []func(*baseapp.BaseApp) {
	if cast.ToBool(appOpts.Get(FlagSeiDB)) {
		logger.Info("Setting up seiDB...")
		// cms must be overridden before the other options, because they may use the cms,
		// make sure the cms aren't be overridden by the other options later on.
		baseAppOptions = append([]func(*baseapp.BaseApp){setupStateCommit()}, baseAppOptions...)
		baseAppOptions = append([]func(*baseapp.BaseApp){setupStateStore()}, baseAppOptions...)
	}

	return baseAppOptions
}

func setupStateCommit() func(*baseapp.BaseApp) {
	return func(baseApp *baseapp.BaseApp) {
		//TODO: Add sc setup
	}
}

func setupStateStore() func(*baseapp.BaseApp) {
	return func(baseApp *baseapp.BaseApp) {
		//TODO: Add SS setup
	}
}
