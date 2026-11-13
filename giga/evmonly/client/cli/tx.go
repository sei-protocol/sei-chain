package cli

import (
	"bufio"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/cobra"

	govprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/input"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec/legacy"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

// maxSlotName is the longest plan name that fits one storage slot.
const maxSlotName = 31

// voteWeightedSig is the voteWeighted overload taking 1e18-scaled weights.
const voteWeightedSig = "voteWeighted(uint64,(int32,uint256)[])"

// submitText is a V2 text proposal, as flags or a --proposal file give it.
type submitText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Deposit     string `json:"deposit"`
	IsExpedited bool   `json:"is_expedited"`
}

func submitTextProposal(cmd *cobra.Command, _ []string) error {
	p, err := readSubmitText(cmd)
	if err != nil {
		return fmt.Errorf("failed to parse proposal: %w", err)
	}
	if normalizeProposalType(p.Type) != govtypes.ProposalTypeText {
		return fmt.Errorf("proposal type %q is not supported with --%s; use %s, or a submit-proposal subcommand", p.Type, FlagEVMRPC, govtypes.ProposalTypeText)
	}
	value, err := depositWei(p.Deposit)
	if err != nil {
		return err
	}
	return sendGov(cmd, value, govprecompile.SubmitTextProposalMethod, p.Title, p.Description, p.IsExpedited)
}

// readSubmitText reads a text proposal from --proposal, or from the other
// submit-proposal flags when it is not set.
func readSubmitText(cmd *cobra.Command) (submitText, error) {
	fs := cmd.Flags()
	file, _ := fs.GetString(flagProposal)
	if file == "" {
		var p submitText
		p.Title, _ = fs.GetString(flagTitle)
		p.Description, _ = fs.GetString(flagDescription)
		p.Deposit, _ = fs.GetString(flagDeposit)
		p.IsExpedited, _ = fs.GetBool(flagIsExpedited)
		p.Type, _ = fs.GetString(flagProposalType)
		return p, nil
	}
	for _, name := range proposalFlags {
		if v, _ := fs.GetString(name); v != "" {
			return submitText{}, fmt.Errorf("--%s flag provided alongside --proposal, which is a noop", name)
		}
	}
	bz, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return submitText{}, err
	}
	var p submitText
	if err := json.Unmarshal(bz, &p); err != nil {
		return submitText{}, err
	}
	return p, nil
}

func submitSoftwareUpgrade(cmd *cobra.Command, args []string) error {
	name := args[0]
	warnLongName(cmd, name)
	height, err := cmd.Flags().GetInt64(flagUpgradeHeight)
	if err != nil {
		return err
	}
	if height <= 0 {
		return fmt.Errorf("--%s must be positive", flagUpgradeHeight)
	}
	info, _ := cmd.Flags().GetString(flagUpgradeInfo)
	title, description, isExpedited, value, err := readProposalFlags(cmd)
	if err != nil {
		return err
	}
	plan := upgradePlan{Name: name, Height: uint64(height), Info: info}
	return sendGov(cmd, value, govprecompile.SubmitSoftwareUpgradeMethod, title, description, isExpedited, plan)
}

func submitCancelSoftwareUpgrade(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString(FlagName)
	if name == "" {
		var err error
		if name, err = confirmScheduledPlan(cmd); err != nil {
			return err
		}
	}
	title, description, isExpedited, value, err := readProposalFlags(cmd)
	if err != nil {
		return err
	}
	return sendGov(cmd, value, govprecompile.SubmitCancelUpgradeMethod, title, description, isExpedited, name)
}

// confirmScheduledPlan returns the scheduled plan's name once the user confirms
// cancelling it, or without asking when --yes is set.
func confirmScheduledPlan(cmd *cobra.Command) (string, error) {
	c, err := dial(cmd)
	if err != nil {
		return "", err
	}
	plan, err := currentPlan(cmd, c)
	if err != nil {
		return "", err
	}
	if plan.Name == "" {
		return "", errors.New("no upgrade scheduled")
	}
	if skip, _ := cmd.Flags().GetBool(flags.FlagSkipConfirmation); skip {
		return plan.Name, nil
	}
	prompt := fmt.Sprintf("Cancel the upgrade %q scheduled at height %d?", plan.Name, plan.Height)
	ok, err := input.GetConfirmation(prompt, bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("cancelled")
	}
	return plan.Name, nil
}

func submitParamChange(cmd *cobra.Command, args []string) error {
	bz, err := os.ReadFile(filepath.Clean(args[0]))
	if err != nil {
		return err
	}
	var p v2ParamChangeProposal
	if err := json.Unmarshal(bz, &p); err != nil {
		return err
	}
	changes, err := flatChanges(p.Changes)
	if err != nil {
		return err
	}
	value, err := depositWei(p.Deposit)
	if err != nil {
		return err
	}
	return sendGov(cmd, value, govprecompile.SubmitParamChangeProposalMethod, p.Title, p.Description, p.IsExpedited, changes)
}

func deposit(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	value, err := depositWei(args[1])
	if err != nil {
		return err
	}
	if value.Sign() == 0 {
		return errors.New("deposit must be positive")
	}
	return sendGov(cmd, value, govprecompile.DepositMethod, id)
}

func vote(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	option, err := govtypes.VoteOptionFromString(normalizeVoteOption(args[1]))
	if err != nil {
		return err
	}
	return sendGov(cmd, nil, govprecompile.VoteMethod, id, int32(option))
}

func weightedVote(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	v2, err := govtypes.WeightedVoteOptionsFromString(normalizeWeightedVoteOptions(args[1]))
	if err != nil {
		return err
	}
	options := make([]weightedOption, len(v2))
	for i, o := range v2 {
		options[i] = weightedOption{Option: int32(o.Option), Weight: scaledFromDec(o.Weight)}
	}
	return sendGov(cmd, nil, voteWeightedSig, id, options)
}

// readProposalFlags reads the title, description, expedited and deposit flags of a
// submit-proposal subcommand, with the deposit in wei.
func readProposalFlags(cmd *cobra.Command) (title, description string, isExpedited bool, value *big.Int, err error) {
	title, _ = cmd.Flags().GetString(flagTitle)
	description, _ = cmd.Flags().GetString(flagDescription)
	isExpedited, _ = cmd.Flags().GetBool(flagIsExpedited)
	d, _ := cmd.Flags().GetString(flagDeposit)
	value, err = depositWei(d)
	return title, description, isExpedited, value, err
}

func parseProposalID(s string) (uint64, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("proposal-id %s not a valid uint, please input a valid proposal-id", s)
	}
	return id, nil
}

// warnLongName warns when a plan name does not fit one storage slot.
func warnLongName(cmd *cobra.Command, name string) {
	if len(name) > maxSlotName {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: plan name %q is %d bytes; names over %d bytes take more storage and gas\n", name, len(name), maxSlotName)
	}
}

// methodBySig returns the method of contract with signature sig, or the method
// named sig.
func methodBySig(contract abi.ABI, sig string) (abi.Method, error) {
	if m, ok := contract.Methods[sig]; ok {
		return m, nil
	}
	for _, m := range contract.Methods {
		if m.Sig == sig {
			return m, nil
		}
	}
	return abi.Method{}, fmt.Errorf("unknown method %s", sig)
}

// sendGov sends a call of method on the gov precompile with value wei from the
// --from key, and prints the result as V2's TxResponse.
func sendGov(cmd *cobra.Command, value *big.Int, method string, args ...any) error {
	m, err := methodBySig(govprecompile.ABI, method)
	if err != nil {
		return err
	}
	input, err := m.Inputs.Pack(args...)
	if err != nil {
		return fmt.Errorf("pack %s: %w", m.Sig, err)
	}
	if err := refuseOfflineModes(cmd); err != nil {
		return err
	}
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	key, err := privateKey(clientCtx)
	if err != nil {
		return err
	}
	opts, err := readTxOpts(cmd)
	if err != nil {
		return err
	}
	c, err := dial(cmd)
	if err != nil {
		return err
	}
	if value == nil {
		value = new(big.Int)
	}
	tx, receipt, err := c.send(cmdContext(cmd), key, govprecompile.Address, value, append(m.ID, input...), opts)
	if err != nil {
		if tx != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "txhash: %s\n", tx.Hash())
		}
		return err
	}
	if err := clientCtx.PrintProto(txResponse(tx, receipt)); err != nil {
		return err
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return fmt.Errorf("transaction %s failed", tx.Hash())
	}
	return nil
}

// refuseOfflineModes rejects the V2 flags that build a transaction without
// broadcasting it.
func refuseOfflineModes(cmd *cobra.Command) error {
	for _, name := range []string{flags.FlagGenerateOnly, flags.FlagDryRun, flags.FlagOffline} {
		if v, _ := cmd.Flags().GetBool(name); v {
			return fmt.Errorf("--%s is not supported with --%s", name, FlagEVMRPC)
		}
	}
	return nil
}

// readTxOpts reads --gas, --gas-adjustment and --gas-fee-cap. --gas is used
// only when set to a number; otherwise the gas is estimated.
func readTxOpts(cmd *cobra.Command) (txOpts, error) {
	var o txOpts
	if flagChanged(cmd, flags.FlagGas) {
		s, _ := cmd.Flags().GetString(flags.FlagGas)
		if s != flags.GasFlagAuto {
			gas, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return o, fmt.Errorf("--%s: %w", flags.FlagGas, err)
			}
			o.gas = gas
		}
	}
	if flagChanged(cmd, flags.FlagGasAdjustment) {
		o.gasAdjustment, _ = cmd.Flags().GetFloat64(flags.FlagGasAdjustment)
	}
	if flagChanged(cmd, FlagGasFeeCap) {
		feeCap, err := cmd.Flags().GetUint64(FlagGasFeeCap)
		if err != nil {
			return o, err
		}
		o.gasPrice = new(big.Int).SetUint64(feeCap)
	}
	return o, nil
}

// privateKey returns the --from key, which must be a local secp256k1 key.
func privateKey(clientCtx client.Context) (*ecdsa.PrivateKey, error) {
	if clientCtx.Keyring == nil {
		return nil, errors.New("no keyring")
	}
	info, err := clientCtx.Keyring.Key(clientCtx.GetFromName())
	if err != nil {
		return nil, err
	}
	local, ok := info.(keyring.LocalInfo)
	if !ok {
		return nil, fmt.Errorf("key %q is not a local key; --%s signs only with local keys", info.GetName(), FlagEVMRPC)
	}
	if local.GetAlgo() != hd.Secp256k1Type {
		return nil, fmt.Errorf("key %q is not secp256k1", info.GetName())
	}
	priv, err := legacy.PrivKeyFromBytes([]byte(local.PrivKeyArmor))
	if err != nil {
		return nil, err
	}
	return crypto.HexToECDSA(hex.EncodeToString(priv.Bytes()))
}

// submitProposalEvent is the gov precompile's SubmitProposal event.
var submitProposalEvent = govprecompile.ABI.Events[govprecompile.SubmitProposalEvent]

// txResponse returns a receipt as V2's TxResponse. A submitted proposal's id is
// in a submit_proposal event's proposal_id attribute, as V2 logs it.
func txResponse(tx *ethtypes.Transaction, r *ethtypes.Receipt) *sdk.TxResponse {
	resp := &sdk.TxResponse{
		TxHash:    tx.Hash().Hex(),
		GasWanted: int64(tx.Gas()),  //nolint:gosec // gas limits fit int64
		GasUsed:   int64(r.GasUsed), //nolint:gosec // gas used fits int64
	}
	if r.BlockNumber != nil {
		resp.Height = r.BlockNumber.Int64()
	}
	if r.Status != ethtypes.ReceiptStatusSuccessful {
		resp.Code = 1
		resp.RawLog = "execution failed"
		return resp
	}
	for _, l := range r.Logs {
		if len(l.Topics) < 2 || l.Topics[0] != submitProposalEvent.ID {
			continue
		}
		id := new(big.Int).SetBytes(l.Topics[1].Bytes()).String()
		resp.Logs = sdk.ABCIMessageLogs{{Events: sdk.StringEvents{{
			Type:       "submit_proposal",
			Attributes: []sdk.Attribute{{Key: "proposal_id", Value: id}},
		}}}}
	}
	return resp
}
