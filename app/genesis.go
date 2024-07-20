package app

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	genesistypes "github.com/cosmos/cosmos-sdk/types/genesis"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

var DefaultGenesisConfig = genesistypes.GenesisImportConfig{
	StreamGenesisImport: false,
	GenesisStreamFile:   "",
}

const (
	flagGenesisStreamImport = "genesis.stream-import"
	flagGenesisImportFile   = "genesis.import-file"
)

func ReadGenesisImportConfig(opts servertypes.AppOptions) (genesistypes.GenesisImportConfig, error) {
	cfg := DefaultGenesisConfig // copy
	var err error
	if v := opts.Get(flagGenesisStreamImport); v != nil {
		if cfg.StreamGenesisImport, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagGenesisImportFile); v != nil {
		cfg.GenesisStreamFile = v.(string)
	}
	return cfg, nil
}

// The genesis state of the blockchain is represented here as a map of raw json
// messages key'd by a identifier string.
// The identifier is used to determine which module genesis information belongs
// to so it may be appropriately routed during init chain.
// Within this application default genesis information is retrieved from
// the ModuleBasicManager which populates json from each BasicModule
// object provided to it during init.
type GenesisState map[string]json.RawMessage

// NewDefaultGenesisState generates the default state for the application.
func NewDefaultGenesisState(cdc codec.JSONCodec) GenesisState {
	encCfg := MakeEncodingConfig()
	gen := ModuleBasics.DefaultGenesis(cdc)

	// Override distribution config to remove community tax
	distrGen := distrtypes.GenesisState{
		Params: distrtypes.Params{
			CommunityTax: sdk.NewDec(0),
		},
	}
	gen[distrtypes.ModuleName] = encCfg.Marshaler.MustMarshalJSON(&distrGen)
	return gen
}
