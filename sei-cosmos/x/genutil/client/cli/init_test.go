package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	genutilcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/client/cli"
	genutiltest "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/client/testutil"
)

var testMbm = module.NewBasicManager(genutil.AppModuleBasic{})

func TestInitCmd(t *testing.T) {
	tests := []struct {
		name      string
		flags     func(dir string) []string
		shouldErr bool
		err       error
	}{
		{
			name: "happy path",
			flags: func(dir string) []string {
				return []string{
					"appnode-test",
				}
			},
			shouldErr: false,
			err:       nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			logger := log.NewNopLogger()
			cfg, err := genutiltest.CreateDefaultTendermintConfig(home)
			require.NoError(t, err)

			serverCtx := server.NewContext(viper.New(), cfg, logger)
			interfaceRegistry := types.NewInterfaceRegistry()
			marshaler := codec.NewProtoCodec(interfaceRegistry)
			clientCtx := client.Context{}.
				WithCodec(marshaler).
				WithLegacyAmino(makeCodec()).
				WithHomeDir(home)

			ctx := context.Background()
			ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
			ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)

			cmd := genutilcli.InitCmd(testMbm, home)
			cmd.SetArgs(
				tt.flags(home),
			)

			if tt.shouldErr {
				err := cmd.ExecuteContext(ctx)
				require.EqualError(t, err, tt.err.Error())
			} else {
				require.NoError(t, cmd.ExecuteContext(ctx))
			}
		})
	}

}

func TestInitRecover(t *testing.T) {
	home := t.TempDir()
	logger := log.NewNopLogger()
	cfg, err := genutiltest.CreateDefaultTendermintConfig(home)
	require.NoError(t, err)

	serverCtx := server.NewContext(viper.New(), cfg, logger)
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	clientCtx := client.Context{}.
		WithCodec(marshaler).
		WithLegacyAmino(makeCodec()).
		WithHomeDir(home)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)

	cmd := genutilcli.InitCmd(testMbm, home)
	mockIn := testutil.ApplyMockIODiscardOutErr(cmd)

	cmd.SetArgs([]string{
		"appnode-test",
		fmt.Sprintf("--%s=true", genutilcli.FlagRecover),
	})

	// use valid mnemonic and complete recovery key generation successfully
	mockIn.Reset("decide praise business actor peasant farm drastic weather extend front hurt later song give verb rhythm worry fun pond reform school tumble august one\n")
	require.NoError(t, cmd.ExecuteContext(ctx))
}

func TestEmptyState(t *testing.T) {
	home := t.TempDir()
	logger := log.NewNopLogger()
	cfg, err := genutiltest.CreateDefaultTendermintConfig(home)
	require.NoError(t, err)

	serverCtx := server.NewContext(viper.New(), cfg, logger)
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	clientCtx := client.Context{}.
		WithCodec(marshaler).
		WithLegacyAmino(makeCodec()).
		WithHomeDir(home)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)

	cmd := genutilcli.InitCmd(testMbm, home)
	cmd.SetArgs([]string{"appnode-test", fmt.Sprintf("--%s=%s", cli.HomeFlag, home)})

	require.NoError(t, cmd.ExecuteContext(ctx))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd = server.ExportCmd(nil, home)
	cmd.SetArgs([]string{fmt.Sprintf("--%s=%s", cli.HomeFlag, home)})
	require.NoError(t, cmd.ExecuteContext(ctx))

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		outC <- buf.String()
	}()

	w.Close()
	os.Stdout = old
	out := <-outC

	require.Contains(t, out, "genesis_time")
	require.Contains(t, out, "chain_id")
	require.Contains(t, out, "consensus_params")
	require.Contains(t, out, "app_hash")
	require.Contains(t, out, "app_state")
}

func TestInitNodeValidatorFiles(t *testing.T) {
	home := t.TempDir()
	cfg, err := genutiltest.CreateDefaultTendermintConfig(home)
	nodeID, valPubKey, err := genutil.InitializeNodeValidatorFiles(cfg)

	require.Nil(t, err)
	require.NotEqual(t, "", nodeID)
	require.NotEqual(t, 0, len(valPubKey.Bytes()))
}

// custom tx codec
func makeCodec() *codec.LegacyAmino {
	var cdc = codec.NewLegacyAmino()
	sdk.RegisterLegacyAminoCodec(cdc)
	cryptocodec.RegisterCrypto(cdc)
	return cdc
}
