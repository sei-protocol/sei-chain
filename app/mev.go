package app

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cast"
	abci "github.com/tendermint/tendermint/abci/types"
)

const pluginObjectName = "HandlerInstance"

type MEVHandler interface {
	Handle(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error)
}

const (
	flagMevHandlerPluginPath = "mev.handler_plugin_path"
)

type MEVConfig struct {
	HandlerPluginPath string `mapstructure:"handler_plugin_path"`
}

var DefaultMEVConfig = MEVConfig{
	HandlerPluginPath: "",
}

func ReadMevConfig(opts servertypes.AppOptions) (MEVConfig, error) {
	cfg := DefaultMEVConfig // copy
	var err error
	if v := opts.Get(flagMevHandlerPluginPath); v != nil {
		if cfg.HandlerPluginPath, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
