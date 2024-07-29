package server

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/spf13/cobra"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	FlagIsStreaming      = "streaming"
	FlagStreamingFile    = "streaming-file"
	FlagHeight           = "height"
	FlagForZeroHeight    = "for-zero-height"
	FlagJailAllowedAddrs = "jail-allowed-addrs"
)

type GenesisDocNoAppState struct {
	GenesisTime     time.Time                  `json:"genesis_time"`
	ChainID         string                     `json:"chain_id"`
	InitialHeight   int64                      `json:"initial_height,string"`
	ConsensusParams *tmtypes.ConsensusParams   `json:"consensus_params,omitempty"`
	Validators      []tmtypes.GenesisValidator `json:"validators,omitempty"`
	AppHash         tmbytes.HexBytes           `json:"app_hash"`
}

// ExportCmd dumps app state to JSON.
func ExportCmd(appExporter types.AppExporter, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export state to JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverCtx := GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			homeDir, _ := cmd.Flags().GetString(flags.FlagHome)
			config.SetRoot(homeDir)

			if _, err := os.Stat(config.GenesisFile()); os.IsNotExist(err) {
				return err
			}

			isStreaming, err := cmd.Flags().GetBool(FlagIsStreaming)
			if err != nil {
				return err
			}

			streamingFile, err := cmd.Flags().GetString(FlagStreamingFile)
			if err != nil {
				return err
			}

			if isStreaming && streamingFile == "" {
				return fmt.Errorf("file to export stream to not provided, please specify --streaming-file")
			}

			db, err := openDB(config.RootDir)
			if err != nil {
				return err
			}

			if appExporter == nil {
				if _, err := fmt.Fprintln(os.Stderr, "WARNING: App exporter not defined. Returning genesis file."); err != nil {
					return err
				}

				genesis, err := ioutil.ReadFile(config.GenesisFile())
				if err != nil {
					return err
				}

				fmt.Println(string(genesis))
				return nil
			}

			traceWriterFile, _ := cmd.Flags().GetString(flagTraceStore)
			traceWriter, err := openTraceWriter(traceWriterFile)
			if err != nil {
				return err
			}

			height, _ := cmd.Flags().GetInt64(FlagHeight)
			forZeroHeight, _ := cmd.Flags().GetBool(FlagForZeroHeight)
			jailAllowedAddrs, _ := cmd.Flags().GetStringSlice(FlagJailAllowedAddrs)

			if isStreaming {
				file, err := os.Create(streamingFile)
				if err != nil {
					return err
				}
				exported, err := appExporter(serverCtx.Logger, db, traceWriter, height, forZeroHeight, jailAllowedAddrs, serverCtx.Viper, file)
				if err != nil {
					return fmt.Errorf("error exporting state: %v", err)
				}

				doc, err := tmtypes.GenesisDocFromFile(serverCtx.Config.GenesisFile())
				if err != nil {
					return err
				}

				genesisDocNoAppHash := GenesisDocNoAppState{
					GenesisTime:   doc.GenesisTime,
					ChainID:       doc.ChainID,
					AppHash:       doc.AppHash,
					InitialHeight: exported.Height,
					ConsensusParams: &tmtypes.ConsensusParams{
						Block: tmtypes.BlockParams{
							MaxBytes: exported.ConsensusParams.Block.MaxBytes,
							MaxGas:   exported.ConsensusParams.Block.MaxGas,
						},
						Evidence: tmtypes.EvidenceParams{
							MaxAgeNumBlocks: exported.ConsensusParams.Evidence.MaxAgeNumBlocks,
							MaxAgeDuration:  exported.ConsensusParams.Evidence.MaxAgeDuration,
							MaxBytes:        exported.ConsensusParams.Evidence.MaxBytes,
						},
						Validator: tmtypes.ValidatorParams{
							PubKeyTypes: exported.ConsensusParams.Validator.PubKeyTypes,
						},
					},
					Validators: exported.Validators,
				}

				// NOTE: Tendermint uses a custom JSON decoder for GenesisDoc
				// (except for stuff inside AppState). Inside AppState, we're free
				// to encode as protobuf or amino.
				encoded, err := json.Marshal(genesisDocNoAppHash)
				if err != nil {
					return err
				}

				file.Write([]byte(fmt.Sprintf("%s", string(sdk.MustSortJSON(encoded)))))
				return nil
			}

			exported, err := appExporter(serverCtx.Logger, db, traceWriter, height, forZeroHeight, jailAllowedAddrs, serverCtx.Viper, nil)
			if err != nil {
				return fmt.Errorf("error exporting state: %v", err)
			}

			doc, err := tmtypes.GenesisDocFromFile(serverCtx.Config.GenesisFile())
			if err != nil {
				return err
			}

			doc.AppState = exported.AppState
			doc.Validators = exported.Validators
			doc.InitialHeight = exported.Height
			doc.ConsensusParams = &tmtypes.ConsensusParams{
				Block: tmtypes.BlockParams{
					MaxBytes: exported.ConsensusParams.Block.MaxBytes,
					MaxGas:   exported.ConsensusParams.Block.MaxGas,
				},
				Evidence: tmtypes.EvidenceParams{
					MaxAgeNumBlocks: exported.ConsensusParams.Evidence.MaxAgeNumBlocks,
					MaxAgeDuration:  exported.ConsensusParams.Evidence.MaxAgeDuration,
					MaxBytes:        exported.ConsensusParams.Evidence.MaxBytes,
				},
				Validator: tmtypes.ValidatorParams{
					PubKeyTypes: exported.ConsensusParams.Validator.PubKeyTypes,
				},
			}

			// NOTE: Tendermint uses a custom JSON decoder for GenesisDoc
			// (except for stuff inside AppState). Inside AppState, we're free
			// to encode as protobuf or amino.
			encoded, err := json.Marshal(doc)
			if err != nil {
				return err
			}

			cmd.Println(string(sdk.MustSortJSON(encoded)))
			return nil
		},
	}

	cmd.Flags().Bool(FlagIsStreaming, false, "Whether to stream the export in chunks. Useful when genesis is extremely large and cannot fit into memory.")
	cmd.Flags().String(FlagStreamingFile, "genesis-stream.json", "The file to export the streamed genesis to")
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().Int64(FlagHeight, -1, "Export state from a particular height (-1 means latest height)")
	cmd.Flags().Bool(FlagForZeroHeight, false, "Export state to start at height zero (perform preproccessing)")
	cmd.Flags().StringSlice(FlagJailAllowedAddrs, []string{}, "Comma-separated list of operator addresses of jailed validators to unjail")
	cmd.Flags().String(FlagChainID, "", "Chain ID")

	return cmd
}
