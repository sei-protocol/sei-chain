package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm/keeper"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/spf13/cobra"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// GenesisReader reads genesis data. Extension point for custom genesis state readers.
type GenesisReader interface {
	ReadWasmGenesis(cmd *cobra.Command) (*GenesisData, error)
}

// GenesisMutator extension point to modify the wasm module genesis state.
// This gives flexibility to customize the data structure in the genesis file a bit.
type GenesisMutator interface {
	// AlterWasmModuleState loads the genesis from the default or set home dir,
	// unmarshalls the wasm module section into the object representation
	// calls the callback function to modify it
	// and marshals the modified state back into the genesis file
	AlterWasmModuleState(cmd *cobra.Command, callback func(state *types.GenesisState, appState map[string]json.RawMessage) error) error
}

// GenesisStoreCodeCmd cli command to add a `MsgStoreCode` to the wasm section of the genesis
// that is executed on block 0.
func GenesisStoreCodeCmd(defaultNodeHome string, genesisMutator GenesisMutator) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store [wasm file] --run-as [owner_address_or_key_name]\",",
		Short: "Upload a wasm binary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			senderAddr, err := getActorAddress(cmd)
			if err != nil {
				return err
			}

			msg, err := parseStoreCodeArgs(args[0], senderAddr, cmd.Flags())
			if err != nil {
				return err
			}
			if err = msg.ValidateBasic(); err != nil {
				return err
			}

			return genesisMutator.AlterWasmModuleState(cmd, func(state *types.GenesisState, _ map[string]json.RawMessage) error {
				state.GenMsgs = append(state.GenMsgs, types.GenesisState_GenMsgs{
					Sum: &types.GenesisState_GenMsgs_StoreCode{StoreCode: &msg},
				})
				return nil
			})
		},
	}
	cmd.Flags().String(flagRunAs, "", "The address that is stored as code creator")
	cmd.Flags().String(flagInstantiateByEverybody, "", "Everybody can instantiate a contract from the code, optional")
	cmd.Flags().String(flagInstantiateNobody, "", "Nobody except the governance process can instantiate a contract from the code, optional")
	cmd.Flags().String(flagInstantiateByAddress, "", "Only this address can instantiate a contract instance from the code, optional")

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test)")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GenesisInstantiateContractCmd cli command to add a `MsgInstantiateContract` to the wasm section of the genesis
// that is executed on block 0.
func GenesisInstantiateContractCmd(defaultNodeHome string, genesisMutator GenesisMutator) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instantiate-contract [code_id_int64] [json_encoded_init_args] --label [text] --run-as [address] --admin [address,optional] --amount [coins,optional]",
		Short: "Instantiate a wasm contract",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			senderAddr, err := getActorAddress(cmd)
			if err != nil {
				return err
			}

			msg, err := parseInstantiateArgs(args[0], args[1], senderAddr, cmd.Flags())
			if err != nil {
				return err
			}
			if err = msg.ValidateBasic(); err != nil {
				return err
			}

			return genesisMutator.AlterWasmModuleState(cmd, func(state *types.GenesisState, appState map[string]json.RawMessage) error {
				// simple sanity check that sender has some balance although it may be consumed by appState previous message already
				switch ok, err := hasAccountBalance(cmd, appState, senderAddr, msg.Funds); {
				case err != nil:
					return err
				case !ok:
					return errors.New("sender has not enough account balance")
				}

				//  does code id exists?
				codeInfos, err := GetAllCodes(state)
				if err != nil {
					return err
				}
				var codeInfo *CodeMeta
				for i := range codeInfos {
					if codeInfos[i].CodeID == msg.CodeID {
						codeInfo = &codeInfos[i]
						break
					}
				}
				if codeInfo == nil {
					return fmt.Errorf("unknown code id: %d", msg.CodeID)
				}
				// permissions correct?
				if !codeInfo.Info.InstantiateConfig.Allowed(senderAddr) {
					return fmt.Errorf("permissions were not granted for %state", senderAddr)
				}
				state.GenMsgs = append(state.GenMsgs, types.GenesisState_GenMsgs{
					Sum: &types.GenesisState_GenMsgs_InstantiateContract{InstantiateContract: &msg},
				})
				return nil
			})
		},
	}
	cmd.Flags().String(flagAmount, "", "Coins to send to the contract during instantiation")
	cmd.Flags().String(flagLabel, "", "A human-readable name for this contract in lists")
	cmd.Flags().String(flagAdmin, "", "Address of an admin")
	cmd.Flags().Bool(flagNoAdmin, false, "You must set this explicitly if you don't want an admin")
	cmd.Flags().String(flagRunAs, "", "The address that pays the init funds. It is the creator of the contract.")

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test)")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GenesisExecuteContractCmd cli command to add a `MsgExecuteContract` to the wasm section of the genesis
// that is executed on block 0.
func GenesisExecuteContractCmd(defaultNodeHome string, genesisMutator GenesisMutator) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute [contract_addr_bech32] [json_encoded_send_args] --run-as [address] --amount [coins,optional]",
		Short: "Execute a command on a wasm contract",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			senderAddr, err := getActorAddress(cmd)
			if err != nil {
				return err
			}

			msg, err := parseExecuteArgs(args[0], args[1], senderAddr, cmd.Flags())
			if err != nil {
				return err
			}
			if err = msg.ValidateBasic(); err != nil {
				return err
			}

			return genesisMutator.AlterWasmModuleState(cmd, func(state *types.GenesisState, appState map[string]json.RawMessage) error {
				// simple sanity check that sender has some balance although it may be consumed by appState previous message already
				switch ok, err := hasAccountBalance(cmd, appState, senderAddr, msg.Funds); {
				case err != nil:
					return err
				case !ok:
					return errors.New("sender has not enough account balance")
				}

				// - does contract address exists?
				if !hasContract(state, msg.Contract) {
					return fmt.Errorf("unknown contract: %state", msg.Contract)
				}
				state.GenMsgs = append(state.GenMsgs, types.GenesisState_GenMsgs{
					Sum: &types.GenesisState_GenMsgs_ExecuteContract{ExecuteContract: &msg},
				})
				return nil
			})
		},
	}
	cmd.Flags().String(flagAmount, "", "Coins to send to the contract along with command")
	cmd.Flags().String(flagRunAs, "", "The address that pays the funds.")

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test)")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GenesisListCodesCmd cli command to list all codes stored in the genesis wasm.code section
// as well as from messages that are queued in the wasm.genMsgs section.
func GenesisListCodesCmd(defaultNodeHome string, genReader GenesisReader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-codes ",
		Short: "Lists all codes from genesis code dump and queued messages",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := genReader.ReadWasmGenesis(cmd)
			if err != nil {
				return err
			}
			all, err := GetAllCodes(g.WasmModuleState)
			if err != nil {
				return err
			}
			return printJSONOutput(cmd, all)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GenesisListContractsCmd cli command to list all contracts stored in the genesis wasm.contract section
// as well as from messages that are queued in the wasm.genMsgs section.
func GenesisListContractsCmd(defaultNodeHome string, genReader GenesisReader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-contracts ",
		Short: "Lists all contracts from genesis contract dump and queued messages",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := genReader.ReadWasmGenesis(cmd)
			if err != nil {
				return err
			}
			state := g.WasmModuleState
			all := GetAllContracts(state)
			return printJSONOutput(cmd, all)
		},
	}
	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// clientCtx marshaller works only with proto or bytes so we marshal the output ourself
func printJSONOutput(cmd *cobra.Command, obj interface{}) error {
	clientCtx := client.GetClientContextFromCmd(cmd)
	bz, err := json.MarshalIndent(obj, "", " ")
	if err != nil {
		return err
	}
	return clientCtx.PrintString(string(bz))
}

type CodeMeta struct {
	CodeID uint64         `json:"code_id"`
	Info   types.CodeInfo `json:"info"`
}

func GetAllCodes(state *types.GenesisState) ([]CodeMeta, error) {
	all := make([]CodeMeta, len(state.Codes))
	for i, c := range state.Codes {
		all[i] = CodeMeta{
			CodeID: c.CodeID,
			Info:   c.CodeInfo,
		}
	}
	// add inflight
	seq := codeSeqValue(state)
	for _, m := range state.GenMsgs {
		if msg := m.GetStoreCode(); msg != nil {
			var accessConfig types.AccessConfig
			if msg.InstantiatePermission != nil {
				accessConfig = *msg.InstantiatePermission
			} else {
				// default
				creator, err := sdk.AccAddressFromBech32(msg.Sender)
				if err != nil {
					return nil, fmt.Errorf("sender: %s", err)
				}
				accessConfig = state.Params.InstantiateDefaultPermission.With(creator)
			}
			hash := sha256.Sum256(msg.WASMByteCode)
			all = append(all, CodeMeta{
				CodeID: seq,
				Info: types.CodeInfo{
					CodeHash:          hash[:],
					Creator:           msg.Sender,
					InstantiateConfig: accessConfig,
				},
			})
			seq++
		}
	}
	return all, nil
}

type ContractMeta struct {
	ContractAddress string             `json:"contract_address"`
	Info            types.ContractInfo `json:"info"`
}

func GetAllContracts(state *types.GenesisState) []ContractMeta {
	all := make([]ContractMeta, len(state.Contracts))
	for i, c := range state.Contracts {
		all[i] = ContractMeta{
			ContractAddress: c.ContractAddress,
			Info:            c.ContractInfo,
		}
	}
	// add inflight
	seq := contractSeqValue(state)
	for _, m := range state.GenMsgs {
		if msg := m.GetInstantiateContract(); msg != nil {
			all = append(all, ContractMeta{
				ContractAddress: keeper.BuildContractAddress(msg.CodeID, seq).String(),
				Info: types.ContractInfo{
					CodeID:  msg.CodeID,
					Creator: msg.Sender,
					Admin:   msg.Admin,
					Label:   msg.Label,
				},
			})
			seq++
		}
	}
	return all
}

func hasAccountBalance(cmd *cobra.Command, appState map[string]json.RawMessage, sender sdk.AccAddress, coins sdk.Coins) (bool, error) {
	// no coins needed, no account needed
	if coins.IsZero() {
		return true, nil
	}
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return false, err
	}
	cdc := clientCtx.Codec
	var genBalIterator banktypes.GenesisBalancesIterator
	err = genutil.ValidateAccountInGenesis(appState, genBalIterator, sender, coins, cdc)
	if err != nil {
		return false, err
	}
	return true, nil
}

func hasContract(state *types.GenesisState, contractAddr string) bool {
	for _, c := range state.Contracts {
		if c.ContractAddress == contractAddr {
			return true
		}
	}
	seq := contractSeqValue(state)
	for _, m := range state.GenMsgs {
		if msg := m.GetInstantiateContract(); msg != nil {
			if keeper.BuildContractAddress(msg.CodeID, seq).String() == contractAddr {
				return true
			}
			seq++
		}
	}
	return false
}

// GenesisData contains raw and unmarshalled data from the genesis file
type GenesisData struct {
	GenesisFile     string
	GenDoc          *tmtypes.GenesisDoc
	AppState        map[string]json.RawMessage
	WasmModuleState *types.GenesisState
}

func NewGenesisData(genesisFile string, genDoc *tmtypes.GenesisDoc, appState map[string]json.RawMessage, wasmModuleState *types.GenesisState) *GenesisData {
	return &GenesisData{GenesisFile: genesisFile, GenDoc: genDoc, AppState: appState, WasmModuleState: wasmModuleState}
}

type DefaultGenesisReader struct{}

func (d DefaultGenesisReader) ReadWasmGenesis(cmd *cobra.Command) (*GenesisData, error) {
	clientCtx := client.GetClientContextFromCmd(cmd)
	serverCtx := server.GetServerContextFromCmd(cmd)
	config := serverCtx.Config
	config.SetRoot(clientCtx.HomeDir)

	genFile := config.GenesisFile()
	appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis state: %w", err)
	}
	var wasmGenesisState types.GenesisState
	if appState[types.ModuleName] != nil {
		clientCtx := client.GetClientContextFromCmd(cmd)
		clientCtx.Codec.MustUnmarshalJSON(appState[types.ModuleName], &wasmGenesisState)
	}

	return NewGenesisData(
		genFile,
		genDoc,
		appState,
		&wasmGenesisState,
	), nil
}

var (
	_ GenesisReader  = DefaultGenesisIO{}
	_ GenesisMutator = DefaultGenesisIO{}
)

// DefaultGenesisIO implements both interfaces to read and modify the genesis state for this module.
// This implementation uses the default data structure that is used by the module.go genesis import/ export.
type DefaultGenesisIO struct {
	DefaultGenesisReader
}

// NewDefaultGenesisIO constructor to create a new instance
func NewDefaultGenesisIO() *DefaultGenesisIO {
	return &DefaultGenesisIO{DefaultGenesisReader: DefaultGenesisReader{}}
}

// AlterWasmModuleState loads the genesis from the default or set home dir,
// unmarshalls the wasm module section into the object representation
// calls the callback function to modify it
// and marshals the modified state back into the genesis file
func (x DefaultGenesisIO) AlterWasmModuleState(cmd *cobra.Command, callback func(state *types.GenesisState, appState map[string]json.RawMessage) error) error {
	g, err := x.ReadWasmGenesis(cmd)
	if err != nil {
		return err
	}
	if err := callback(g.WasmModuleState, g.AppState); err != nil {
		return err
	}
	// and store update
	if err := g.WasmModuleState.ValidateBasic(); err != nil {
		return err
	}
	clientCtx := client.GetClientContextFromCmd(cmd)
	wasmGenStateBz, err := clientCtx.Codec.MarshalJSON(g.WasmModuleState)
	if err != nil {
		return sdkerrors.Wrap(err, "marshal wasm genesis state")
	}

	g.AppState[types.ModuleName] = wasmGenStateBz
	appStateJSON, err := json.Marshal(g.AppState)
	if err != nil {
		return sdkerrors.Wrap(err, "marshal application genesis state")
	}

	g.GenDoc.AppState = appStateJSON
	return genutil.ExportGenesisFile(g.GenDoc, g.GenesisFile)
}

// contractSeqValue reads the contract sequence from the genesis or
// returns default start value used in the keeper
func contractSeqValue(state *types.GenesisState) uint64 {
	var seq uint64 = 1
	for _, s := range state.Sequences {
		if bytes.Equal(s.IDKey, types.KeyLastInstanceID) {
			seq = s.Value
			break
		}
	}
	return seq
}

// codeSeqValue reads the code sequence from the genesis or
// returns default start value used in the keeper
func codeSeqValue(state *types.GenesisState) uint64 {
	var seq uint64 = 1
	for _, s := range state.Sequences {
		if bytes.Equal(s.IDKey, types.KeyLastCodeID) {
			seq = s.Value
			break
		}
	}
	return seq
}

// getActorAddress returns the account address for the `--run-as` flag.
// The flag value can either be an address already or a key name where the
// address is read from the keyring instead.
func getActorAddress(cmd *cobra.Command) (sdk.AccAddress, error) {
	actorArg, err := cmd.Flags().GetString(flagRunAs)
	if err != nil {
		return nil, fmt.Errorf("run-as: %s", err.Error())
	}
	if len(actorArg) == 0 {
		return nil, errors.New("run-as address is required")
	}

	actorAddr, err := sdk.AccAddressFromBech32(actorArg)
	if err == nil {
		return actorAddr, nil
	}
	inBuf := bufio.NewReader(cmd.InOrStdin())
	keyringBackend, err := cmd.Flags().GetString(flags.FlagKeyringBackend)
	if err != nil {
		return nil, err
	}

	homeDir := client.GetClientContextFromCmd(cmd).HomeDir
	// attempt to lookup address from Keybase if no address was provided
	kb, err := keyring.New(sdk.KeyringServiceName(), keyringBackend, homeDir, inBuf)
	if err != nil {
		return nil, err
	}

	info, err := kb.Key(actorArg)
	if err != nil {
		return nil, fmt.Errorf("failed to get address from Keybase: %w", err)
	}
	return info.GetAddress(), nil
}
