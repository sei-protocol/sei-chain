package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/spf13/cobra"
	abciclient "github.com/tendermint/tendermint/abci/client"
	abci "github.com/tendermint/tendermint/abci/types"
	tmcfg "github.com/tendermint/tendermint/config"
	tmexport "github.com/tendermint/tendermint/export"
	tmjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/privval"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"
)

const (
	KeyIsTestnet      = "is-testnet"
	KeyNewChainID     = "new-chain-ID"
	KeyNewValAddr     = "new-validator-addr"
	KeyUserPubKey     = "user-pub-key"
	FlagShutdownGrace = "shutdown-grace"
)

// Copied from osmosis-lab/cosmos-sdk
//
// InPlaceTestnetCreator utilizes the provided chainID and operatorAddress as well as the local private validator key to
// control the network represented in the data folder. This is useful to create testnets nearly identical to your
// mainnet environment.
func InPlaceTestnetCreator(testnetAppCreator types.AppCreator, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "in-place-testnet [newChainID] [newOperatorAddress]",
		Short: "Create and start a testnet from current local state",
		Long: `Create and start a testnet from current local state.
After utilizing this command the network will start. If the network is stopped,
the normal "start" command should be used. Re-using this command on state that
has already been modified by this command could result in unexpected behavior.

Additionally, the first block may take up to one minute to be committed, depending
on how old the block is. For instance, if a snapshot was taken weeks ago and we want
to turn this into a testnet, it is possible lots of pending state needs to be committed
(expiring locks, etc.). It is recommended that you should wait for this block to be committed
before stopping the daemon.

If the --trigger-testnet-upgrade flag is set, the upgrade handler specified by the flag will be run
on the first block of the testnet.

Regardless of whether the flag is set or not, if any new stores are introduced in the daemon being run,
those stores will be registered in order to prevent panics. Therefore, you only need to set the flag if
you want to test the upgrade handler itself.
`,
		Example: "in-place-testnet localsei",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defer func() {
				if e := recover(); e != nil {
					debug.PrintStack()
					panic(e)
				}
			}()
			serverCtx := GetServerContextFromCmd(cmd)
			_, err := GetPruningOptionsFromFlags(serverCtx.Viper)
			if err != nil {
				return err
			}

			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			newChainID := args[0]

			skipConfirmation, _ := cmd.Flags().GetBool("skip-confirmation")

			if !skipConfirmation {
				// Confirmation prompt to prevent accidental modification of state.
				reader := bufio.NewReader(os.Stdin)
				fmt.Println("This operation will modify state in your data folder and cannot be undone. Do you want to continue? (y/n)")
				text, _ := reader.ReadString('\n')
				response := strings.TrimSpace(strings.ToLower(text))
				if response != "y" && response != "yes" {
					fmt.Println("Operation canceled.")
					return nil
				}
			}

			// Set testnet keys to be used by the application.
			// This is done to prevent changes to existing start API.
			serverCtx.Viper.Set(KeyIsTestnet, true)
			serverCtx.Viper.Set(KeyNewChainID, newChainID)

			config, _ := config.GetConfig(serverCtx.Viper)
			apiMetrics, err := telemetry.New(config.Telemetry)
			if err != nil {
				return fmt.Errorf("failed to initialize telemetry: %w", err)
			}
			restartCoolDownDuration := time.Second * time.Duration(serverCtx.Config.SelfRemediation.RestartCooldownSeconds)
			// Set the first restart time to be now - restartCoolDownDuration so that the first restart can trigger whenever
			canRestartAfter := time.Now().Add(-restartCoolDownDuration)
			err = startInProcess(
				serverCtx,
				clientCtx,
				func(l log.Logger, d dbm.DB, w io.Writer, c *tmcfg.Config, ao types.AppOptions) types.Application {
					testApp, err := testnetify(serverCtx, testnetAppCreator, d, w)
					if err != nil {
						panic(err)
					}
					return testApp
				},
				[]trace.TracerProviderOption{},
				node.DefaultMetricsProvider(serverCtx.Config.Instrumentation)(clientCtx.ChainID),
				apiMetrics,
				canRestartAfter,
			)

			serverCtx.Logger.Debug("received quit signal")
			graceDuration, _ := cmd.Flags().GetDuration(FlagShutdownGrace)
			if graceDuration > 0 {
				serverCtx.Logger.Info("graceful shutdown start", FlagShutdownGrace, graceDuration)
				<-time.After(graceDuration)
				serverCtx.Logger.Info("graceful shutdown complete")
			}

			return err
		},
	}

	addStartNodeFlags(cmd, defaultNodeHome)
	cmd.Flags().Bool("skip-confirmation", false, "Skip the confirmation prompt")
	return cmd
}

// testnetify modifies both state and blockStore, allowing the provided operator address and local validator key to control the network
// that the state in the data folder represents. The chainID of the local genesis file is modified to match the provided chainID.
func testnetify(ctx *Context, testnetAppCreator types.AppCreator, db dbm.DB, traceWriter io.Writer) (types.Application, error) {
	config := ctx.Config

	newChainID, ok := ctx.Viper.Get(KeyNewChainID).(string)
	if !ok {
		return nil, fmt.Errorf("expected string for key %s", KeyNewChainID)
	}

	// Modify app genesis chain ID and save to genesis file.
	genFilePath := config.GenesisFile()
	genDoc, err := tmtypes.GenesisDocFromFile(config.GenesisFile())
	if err != nil {
		panic(err)
	}
	genDoc.ChainID = newChainID

	if err := genDoc.ValidateAndComplete(); err != nil {
		panic(err)
	}
	if err := genDoc.SaveAs(genFilePath); err != nil {
		panic(err)
	}

	// Regenerate addrbook.json to prevent peers on old network from causing error logs.
	addrBookPath := filepath.Join(config.RootDir, "config", "addrbook.json")
	if err := os.Remove(addrBookPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove existing addrbook.json: %w", err)
	}

	emptyAddrBook := []byte("{}")
	if err := os.WriteFile(addrBookPath, emptyAddrBook, 0o600); err != nil {
		return nil, fmt.Errorf("failed to create empty addrbook.json: %w", err)
	}

	// Initialize blockStore and stateDB.
	blockStoreDB, err := tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "blockstore", Config: config})
	if err != nil {
		panic(err)
	}
	blockStore := tmexport.NewBlockStore(blockStoreDB)

	stateDB, err := tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "state", Config: config})
	if err != nil {
		panic(err)
	}

	privValidator, err := privval.LoadOrGenFilePV(ctx.Config.PrivValidator.KeyFile(), ctx.Config.PrivValidator.StateFile())
	if err != nil {
		panic(err)
	}
	userPubKey, err := privValidator.GetPubKey(context.Background())
	if err != nil {
		panic(err)
	}
	validatorAddress := userPubKey.Address()

	stateStore := tmexport.NewStore(stateDB)

	state, err := node.LoadStateFromDBOrGenesisDocProvider(stateStore, genDoc)
	if err != nil {
		panic(err)
	}

	blockStore.Close()
	stateDB.Close()

	ctx.Viper.Set(KeyNewValAddr, validatorAddress)
	ctx.Viper.Set(KeyUserPubKey, userPubKey)
	ctx.Viper.Set(FlagChainID, newChainID)
	testnetApp := testnetAppCreator(ctx.Logger, db, traceWriter, ctx.Config, ctx.Viper)

	// We need to create a temporary proxyApp to get the initial state of the application.
	// Depending on how the node was stopped, the application height can differ from the blockStore height.
	// This height difference changes how we go about modifying the state.
	localClient := abciclient.NewLocalClient(ctx.Logger, testnetApp)
	res, err := localClient.Info(context.Background(), &abci.RequestInfo{})
	if err != nil {
		return nil, fmt.Errorf("error calling Info: %v", err)
	}

	blockStoreDB, err = tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "blockstore", Config: config})
	if err != nil {
		panic(err)
	}
	blockStore = tmexport.NewBlockStore(blockStoreDB)

	stateDB, err = tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "state", Config: config})
	if err != nil {
		panic(err)
	}

	stateStore = tmexport.NewStore(stateDB)

	defer blockStore.Close()
	defer stateStore.Close()

	appHash := res.LastBlockAppHash
	appHeight := res.LastBlockHeight

	var block *tmtypes.Block
	switch {
	case appHeight == blockStore.Height():
		block = blockStore.LoadBlock(blockStore.Height())
		// If the state's last blockstore height does not match the app and blockstore height, we likely stopped with the halt height flag.
		if state.LastBlockHeight != appHeight {
			state.LastBlockHeight = appHeight
			block.AppHash = appHash
			state.AppHash = appHash
		} else {
			// Node was likely stopped via SIGTERM, delete the next block's seen commit
			err := blockStoreDB.Delete([]byte(fmt.Sprintf("SC:%v", blockStore.Height()+1)))
			if err != nil {
				panic(err)
			}
		}
	case blockStore.Height() > state.LastBlockHeight:
		// This state usually occurs when we gracefully stop the node.
		err = blockStore.DeleteLatestBlock()
		if err != nil {
			panic(err)
		}
		block = blockStore.LoadBlock(blockStore.Height())
	default:
		// If there is any other state, we just load the block
		block = blockStore.LoadBlock(blockStore.Height())
	}

	block.ChainID = newChainID
	state.ChainID = newChainID

	block.LastBlockID = state.LastBlockID
	block.LastCommit.BlockID = state.LastBlockID

	// Create a vote from our validator
	vote := tmtypes.Vote{
		Type:             tmproto.PrecommitType,
		Height:           state.LastBlockHeight,
		Round:            0,
		BlockID:          state.LastBlockID,
		Timestamp:        time.Now(),
		ValidatorAddress: validatorAddress,
		ValidatorIndex:   0,
		Signature:        []byte{},
	}

	// Sign the vote, and copy the proto changes from the act of signing to the vote itself
	voteProto := vote.ToProto()
	privValidator.LastSignState.Round = 0
	privValidator.LastSignState.Step = 0
	if privValidator.LastSignState.Height > state.LastBlockHeight {
		privValidator.LastSignState.Height = state.LastBlockHeight
	}
	err = privValidator.SignVote(context.Background(), newChainID, voteProto)
	if err != nil {
		panic(err)
	}
	vote.Signature = voteProto.Signature
	vote.Timestamp = voteProto.Timestamp

	// Modify the block's lastCommit to be signed only by our validator
	block.LastCommit.Signatures[0].ValidatorAddress = validatorAddress
	block.LastCommit.Signatures[0].Signature = vote.Signature
	block.LastCommit.Signatures = []tmtypes.CommitSig{block.LastCommit.Signatures[0]}

	seenCommit := tmtypes.Commit{}
	seenCommit.Height = state.LastBlockHeight
	seenCommit.Round = vote.Round
	seenCommit.BlockID = state.LastBlockID
	seenCommit.Round = vote.Round
	seenCommit.Signatures = []tmtypes.CommitSig{{}}
	seenCommit.Signatures[0].BlockIDFlag = tmtypes.BlockIDFlagCommit
	seenCommit.Signatures[0].Signature = vote.Signature
	seenCommit.Signatures[0].ValidatorAddress = validatorAddress
	seenCommit.Signatures[0].Timestamp = vote.Timestamp
	err = blockStore.SaveSeenCommit(state.LastBlockHeight, &seenCommit)
	if err != nil {
		panic(err)
	}

	// Create ValidatorSet struct containing just our valdiator.
	newVal := &tmtypes.Validator{
		Address:     validatorAddress,
		PubKey:      userPubKey,
		VotingPower: 900000000000000,
	}
	newValSet := &tmtypes.ValidatorSet{
		Validators: []*tmtypes.Validator{newVal},
		Proposer:   newVal,
	}

	// Replace all valSets in state to be the valSet with just our validator.
	state.Validators = newValSet
	state.LastValidators = newValSet
	state.NextValidators = newValSet
	state.LastHeightValidatorsChanged = blockStore.Height()

	err = stateStore.Save(state)
	if err != nil {
		panic(err)
	}

	// Modfiy Validators stateDB entry.
	stateStore.SaveValidatorSets(blockStore.Height()-1, blockStore.Height()-1, newValSet)
	stateStore.SaveValidatorSets(blockStore.Height(), blockStore.Height(), newValSet)
	stateStore.SaveValidatorSets(blockStore.Height()+1, blockStore.Height()+1, newValSet)

	// Since we modified the chainID, we set the new genesisDoc in the stateDB.
	b, err := tmjson.Marshal(genDoc)
	if err != nil {
		panic(err)
	}
	if err := stateDB.SetSync([]byte("genesisDoc"), b); err != nil {
		panic(err)
	}

	testnetApp.InplaceTestnetInitialize(&ed25519.PubKey{Key: userPubKey.Bytes()})

	return testnetApp, err
}
