package staking

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// LoadApp opens the application at the latest committed height, or at height when
// height is positive.
func LoadApp(cmd *cobra.Command, height int64) (*app.App, error) {
	serverCtx := server.GetServerContextFromCmd(cmd)
	home := cast.ToString(serverCtx.Viper.Get(flags.FlagHome))
	if home == "" {
		return nil, fmt.Errorf("home directory is required")
	}

	encCfg := app.MakeEncodingConfig()
	encCfg.Marshaler = codec.NewProtoCodec(encCfg.InterfaceRegistry)

	seiApp := app.New(
		nil,
		nil,
		false,
		map[int64]bool{},
		home,
		1,
		true,
		serverCtx.Config,
		encCfg,
		app.GetWasmEnabledProposals(),
		serverCtx.Viper,
		app.EmptyWasmOpts,
		app.EmptyAppOptions,
	)
	if height > 0 {
		if err := seiApp.LoadHeight(height); err != nil {
			return nil, fmt.Errorf("load height %d: %w", height, err)
		}
	}
	return seiApp, nil
}

// BindServerFlags wires the home flag for offline app commands. Config loading
// is handled by the root seid PersistentPreRunE.
func BindServerFlags(cmd *cobra.Command, defaultHome string) {
	cmd.PersistentFlags().String(flags.FlagHome, defaultHome, "The application home directory")
}
