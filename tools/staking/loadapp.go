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

	var seiApp *app.App
	if height > 0 {
		seiApp = app.New(
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
		if err := seiApp.LoadHeight(height); err != nil {
			return nil, fmt.Errorf("load height %d: %w", height, err)
		}
		return seiApp, nil
	}

	seiApp = app.New(
		nil,
		nil,
		true,
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
	return seiApp, nil
}

// BindServerFlags wires home and config loading for offline app commands.
func BindServerFlags(cmd *cobra.Command, defaultHome string) {
	cmd.PersistentFlags().String(flags.FlagHome, defaultHome, "The application home directory")
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return server.InterceptConfigsPreRunHandler(cmd, "", nil)
	}
}
