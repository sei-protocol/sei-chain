package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tmcommands "github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel/sdk/trace"
)

const (
	homeFlag         = "home"
	freezeHeightFlag = "freeze-height"
)

var logger = seilog.NewLogger("cmd", "giga")

type nodeRunner func(context.Context, *tmconfig.Config, uint64) error

// Execute runs the giga command.
func Execute() error {
	root, err := NewRootCmd()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return err
	}
	return nil
}

// NewRootCmd creates the standalone EVM-only node command.
func NewRootCmd() (*cobra.Command, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	return newRootCmd(filepath.Join(home, ".sei"), runNode), nil
}

func newRootCmd(defaultHome string, run nodeRunner) *cobra.Command {
	root := &cobra.Command{
		Use:           "giga",
		Short:         "Run the standalone EVM-only node",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().String(homeFlag, defaultHome, "directory for node configuration and data")

	start := &cobra.Command{
		Use:   "start",
		Short: "Start the EVM-only node",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config, err := loadNodeConfig(cmd)
			if err != nil {
				return err
			}
			if err := setLogLevel(config.LogLevel); err != nil {
				return err
			}
			freezeHeight, err := cmd.Flags().GetUint64(freezeHeightFlag)
			if err != nil {
				return err
			}
			return run(cmd.Context(), config, freezeHeight)
		},
	}
	tmcommands.AddNodeFlags(start, tmconfig.DefaultConfig())
	start.Flags().Uint64(freezeHeightFlag, 0, "block height before which a full node stops execution")
	root.AddCommand(start)
	return root
}

func loadNodeConfig(cmd *cobra.Command) (*tmconfig.Config, error) {
	source := viper.New()
	source.SetEnvPrefix("GIGA")
	source.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	source.AutomaticEnv()
	if err := source.BindPFlags(cmd.Flags()); err != nil {
		return nil, fmt.Errorf("bind command flags: %w", err)
	}
	if err := source.BindPFlags(cmd.InheritedFlags()); err != nil {
		return nil, fmt.Errorf("bind persistent flags: %w", err)
	}

	home := source.GetString(homeFlag)
	configFile := filepath.Join(home, "config", "config.toml")
	source.SetConfigFile(configFile)
	if err := source.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read node config %s: %w", configFile, err)
	}

	config := tmconfig.DefaultConfig()
	if err := source.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("parse node config %s: %w", configFile, err)
	}
	config.SetRoot(home)
	config.EVMOnly = true
	if err := config.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("validate node config %s: %w", configFile, err)
	}
	return config, nil
}

func setLogLevel(value string) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return fmt.Errorf("parse log level %q: %w", value, err)
	}
	seilog.SetDefaultLevel(level, true)
	return nil
}

func runNode(ctx context.Context, config *tmconfig.Config, freezeHeight uint64) error {
	for {
		restart := make(chan struct{}, 1)
		restartEvent := func() {
			select {
			case restart <- struct{}{}:
			default:
			}
		}
		service, err := node.NewEVMOnly(
			ctx,
			config,
			restartEvent,
			nil,
			[]trace.TracerProviderOption{},
			tmtypes.DefaultConsensusPolicy(),
			node.WithFreezeHeight(freezeHeight),
		)
		if err != nil {
			return fmt.Errorf("create EVM-only node: %w", err)
		}
		if err := service.Start(ctx); err != nil {
			return fmt.Errorf("start EVM-only node: %w", err)
		}

		select {
		case <-ctx.Done():
			service.Wait()
			return nil
		case <-restart:
			service.Stop()
			if ctx.Err() != nil {
				return nil
			}
			logger.Info("restarting EVM-only node")
		}
	}
}
