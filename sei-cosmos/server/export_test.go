package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmtypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
)

func TestExportCmd_ConsensusParams(t *testing.T) {
	tempDir := t.TempDir()

	_, ctx, _, cmd := setupApp(t, tempDir)

	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetArgs([]string{fmt.Sprintf("--%s=%s", flags.FlagHome, tempDir)})
	require.NoError(t, cmd.ExecuteContext(ctx))

	var exportedGenDoc tmtypes.GenesisDoc
	err := json.Unmarshal(output.Bytes(), &exportedGenDoc)
	if err != nil {
		t.Fatalf("error unmarshaling exported genesis doc: %s", err)
	}

	require.Equal(t, simapp.DefaultConsensusParams.Block.MaxBytes, exportedGenDoc.ConsensusParams.Block.MaxBytes)
	require.Equal(t, simapp.DefaultConsensusParams.Block.MaxGas, exportedGenDoc.ConsensusParams.Block.MaxGas)

	require.Equal(t, simapp.DefaultConsensusParams.Evidence.MaxAgeDuration, exportedGenDoc.ConsensusParams.Evidence.MaxAgeDuration)
	require.Equal(t, simapp.DefaultConsensusParams.Evidence.MaxAgeNumBlocks, exportedGenDoc.ConsensusParams.Evidence.MaxAgeNumBlocks)

	require.Equal(t, simapp.DefaultConsensusParams.Validator.PubKeyTypes, exportedGenDoc.ConsensusParams.Validator.PubKeyTypes)
}

func TestExportCmd_HomeDir(t *testing.T) {
	_, ctx, _, cmd := setupApp(t, t.TempDir())

	cmd.SetArgs([]string{fmt.Sprintf("--%s=%s", flags.FlagHome, "foobar")})

	err := cmd.ExecuteContext(ctx)
	require.EqualError(t, err, "stat foobar/config/genesis.json: no such file or directory")
}

func TestExportCmd_Height(t *testing.T) {
	testCases := []struct {
		name        string
		flags       []string
		fastForward int64
		expHeight   int64
	}{
		{
			"should export correct height",
			[]string{},
			5, 6,
		},
		{
			"should export correct height with --height",
			[]string{
				fmt.Sprintf("--%s=%d", server.FlagHeight, 3),
			},
			5, 4,
		},
		{
			"should export height 0 with --for-zero-height",
			[]string{
				fmt.Sprintf("--%s=%s", server.FlagForZeroHeight, "true"),
			},
			2, 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			app, ctx, _, cmd := setupApp(t, tempDir)

			// Fast forward to block `tc.fastForward`.
			for i := int64(2); i <= tc.fastForward; i++ {
				app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: i})
				app.Commit(context.Background())
			}

			output := &bytes.Buffer{}
			cmd.SetOut(output)
			args := append(tc.flags, fmt.Sprintf("--%s=%s", flags.FlagHome, tempDir))
			cmd.SetArgs(args)
			require.NoError(t, cmd.ExecuteContext(ctx))

			var exportedGenDoc tmtypes.GenesisDoc
			err := json.Unmarshal(output.Bytes(), &exportedGenDoc)
			if err != nil {
				t.Fatalf("error unmarshaling exported genesis doc: %s", err)
			}

			require.Equal(t, tc.expHeight, exportedGenDoc.InitialHeight)
		})
	}

}

func setupApp(t *testing.T, tempDir string) (*app.App, context.Context, *tmtypes.GenesisDoc, *cobra.Command) {
	if err := createConfigFolder(tempDir); err != nil {
		t.Fatalf("error creating config folder: %s", err)
	}

	encCfg := simapp.MakeTestEncodingConfig()
	a := app.Setup(false, false, false)

	serverCtx := server.NewDefaultContext()
	serverCtx.Config.RootDir = tempDir

	clientCtx := client.Context{}.WithCodec(a.AppCodec())
	genDoc := newDefaultGenesisDoc(encCfg.Marshaler)

	require.NoError(t, saveGenesisFile(genDoc, serverCtx.Config.GenesisFile()))
	a.InitChain(
		context.Background(), &abci.RequestInitChain{
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: app.DefaultConsensusParams,
			AppStateBytes:   genDoc.AppState,
		},
	)
	a.SetDeliverStateToCommit()
	a.Commit(context.Background())

	cmd := server.ExportCmd(
		func(_ log.Logger, _ dbm.DB, _ io.Writer, height int64, forZeroHeight bool, jailAllowedAddrs []string, appOptons types.AppOptions, file *os.File) (types.ExportedApp, error) {

			var aapp *app.App
			if height != -1 {
				aapp = app.Setup(false, false, false)

				if err := aapp.LoadHeight(height); err != nil {
					return types.ExportedApp{}, err
				}
			} else {
				aapp = app.Setup(false, false, false)
			}

			return aapp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs)
		}, tempDir)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	ctx = context.WithValue(ctx, server.ServerContextKey, serverCtx)

	return a, ctx, genDoc, cmd
}

func createConfigFolder(dir string) error {
	return os.Mkdir(path.Join(dir, "config"), 0700)
}

func newDefaultGenesisDoc(cdc codec.Codec) *tmtypes.GenesisDoc {
	genesisState := simapp.NewDefaultGenesisState(cdc)

	stateBytes, err := json.MarshalIndent(genesisState, "", "  ")
	if err != nil {
		panic(err)
	}

	genDoc := &tmtypes.GenesisDoc{}
	genDoc.ChainID = "theChainId"
	genDoc.Validators = nil
	genDoc.AppState = stateBytes

	return genDoc
}

func saveGenesisFile(genDoc *tmtypes.GenesisDoc, dir string) error {
	err := genutil.ExportGenesisFile(genDoc, dir)
	if err != nil {
		return errors.Wrap(err, "error creating file")
	}

	return nil
}
