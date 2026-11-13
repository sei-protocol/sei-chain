package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/spf13/cobra"

	govprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	upgradeprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramproposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

// defaultPageLimit is how many results a paged query collects when --limit is
// not set.
const defaultPageLimit = 100

// queryCtx returns the client context and EVM client of a query.
func queryCtx(cmd *cobra.Command) (client.Context, *evmClient, error) {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return client.Context{}, nil, err
	}
	c, err := dial(cmd)
	if err != nil {
		return client.Context{}, nil, err
	}
	return clientCtx, c, nil
}

// callInto calls method and converts its single output into T.
func callInto[T any](cmd *cobra.Command, c *evmClient, to common.Address, contract abi.ABI, method string, args ...any) (T, error) {
	var out T
	vals, err := c.call(cmdContext(cmd), to, contract, method, args...)
	if err != nil {
		return out, err
	}
	return *abi.ConvertType(vals[0], new(T)).(*T), nil
}

func queryProposal(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	p, err := callInto[proposalData](cmd, c, govprecompile.Address, govprecompile.ABI, govprecompile.ProposalMethod, id)
	if err != nil {
		return err
	}
	v2, err := v2Proposal(p)
	if err != nil {
		return err
	}
	return clientCtx.PrintProto(&v2)
}

func queryProposals(cmd *cobra.Command, _ []string) error {
	var status int32
	if s, _ := cmd.Flags().GetString(flagStatus); s != "" {
		v, err := govtypes.ProposalStatusFromString(normalizeProposalStatus(s))
		if err != nil {
			return err
		}
		status = int32(v)
	}
	voter, err := addressFlag(cmd, flagVoter)
	if err != nil {
		return err
	}
	depositor, err := addressFlag(cmd, flagDepositor)
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	var out []govtypes.Proposal
	next, err := collectPages(cmd, func(key []byte) (int, []byte, error) {
		vals, err := c.call(cmdContext(cmd), govprecompile.Address, govprecompile.ABI, govprecompile.ProposalsMethod, status, voter, depositor, key)
		if err != nil {
			return 0, nil, err
		}
		page := *abi.ConvertType(vals[0], new([]proposalData)).(*[]proposalData)
		for _, p := range page {
			v2, err := v2Proposal(p)
			if err != nil {
				return 0, nil, err
			}
			out = append(out, v2)
		}
		return len(page), vals[1].([]byte), nil
	})
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return errors.New("no proposals found")
	}
	return clientCtx.PrintProto(&govtypes.QueryProposalsResponse{Proposals: out, Pagination: &query.PageResponse{NextKey: next}})
}

func queryVote(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	voter, err := parseAddress(args[1])
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	v, err := callInto[voteData](cmd, c, govprecompile.Address, govprecompile.ABI, govprecompile.GetVoteMethod, id, voter)
	if err != nil {
		return err
	}
	out := v2Vote(v)
	return clientCtx.PrintProto(&out)
}

func queryVotes(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	var out []govtypes.Vote
	next, err := collectPages(cmd, func(key []byte) (int, []byte, error) {
		vals, err := c.call(cmdContext(cmd), govprecompile.Address, govprecompile.ABI, govprecompile.VotesMethod, id, key)
		if err != nil {
			return 0, nil, err
		}
		page := *abi.ConvertType(vals[0], new([]voteData)).(*[]voteData)
		for _, v := range page {
			out = append(out, v2Vote(v))
		}
		return len(page), vals[1].([]byte), nil
	})
	if err != nil {
		return err
	}
	return clientCtx.PrintProto(&govtypes.QueryVotesResponse{Votes: out, Pagination: &query.PageResponse{NextKey: next}})
}

func queryDeposit(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	depositor, err := parseAddress(args[1])
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	d, err := callInto[depositData](cmd, c, govprecompile.Address, govprecompile.ABI, govprecompile.GetDepositMethod, id, depositor)
	if err != nil {
		return err
	}
	out := v2Deposit(d)
	return clientCtx.PrintProto(&out)
}

func queryDeposits(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	var out []govtypes.Deposit
	next, err := collectPages(cmd, func(key []byte) (int, []byte, error) {
		vals, err := c.call(cmdContext(cmd), govprecompile.Address, govprecompile.ABI, govprecompile.DepositsMethod, id, key)
		if err != nil {
			return 0, nil, err
		}
		page := *abi.ConvertType(vals[0], new([]depositData)).(*[]depositData)
		for _, d := range page {
			out = append(out, v2Deposit(d))
		}
		return len(page), vals[1].([]byte), nil
	})
	if err != nil {
		return err
	}
	return clientCtx.PrintProto(&govtypes.QueryDepositsResponse{Deposits: out, Pagination: &query.PageResponse{NextKey: next}})
}

func queryTally(cmd *cobra.Command, args []string) error {
	id, err := parseProposalID(args[0])
	if err != nil {
		return err
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	t, err := callInto[tallyData](cmd, c, govprecompile.Address, govprecompile.ABI, govprecompile.TallyResultMethod, id)
	if err != nil {
		return err
	}
	out := v2Tally(t)
	return clientCtx.PrintProto(&out)
}

// govParams reads gov's params as V2's.
func govParams(cmd *cobra.Command, c *evmClient) (govtypes.Params, error) {
	p, err := callInto[govParamsData](cmd, c, govprecompile.Address, govprecompile.ABI, govprecompile.ParamsMethod)
	if err != nil {
		return govtypes.Params{}, err
	}
	return v2GovParams(p)
}

func queryGovParams(cmd *cobra.Command, _ []string) error {
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	p, err := govParams(cmd, c)
	if err != nil {
		return err
	}
	return clientCtx.PrintObjectLegacy(p)
}

func queryGovParam(cmd *cobra.Command, args []string) error {
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	p, err := govParams(cmd, c)
	if err != nil {
		return err
	}
	switch args[0] {
	case "voting":
		return clientCtx.PrintObjectLegacy(p.VotingParams)
	case "tallying":
		return clientCtx.PrintObjectLegacy(p.TallyParams)
	case "deposit":
		return clientCtx.PrintObjectLegacy(p.DepositParams)
	default:
		return fmt.Errorf("argument must be one of (voting|tallying|deposit), was %s", args[0])
	}
}

// currentPlan returns the scheduled upgrade plan, with an empty name when none
// is scheduled.
func currentPlan(cmd *cobra.Command, c *evmClient) (upgradePlan, error) {
	return callInto[upgradePlan](cmd, c, upgradeprecompile.Address, upgradeprecompile.ABI, upgradeprecompile.CurrentPlanMethod)
}

func queryPlan(cmd *cobra.Command, _ []string) error {
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	plan, err := currentPlan(cmd, c)
	if err != nil {
		return err
	}
	if plan.Name == "" {
		return errors.New("no upgrade scheduled")
	}
	height, err := toInt64(plan.Height)
	if err != nil {
		return err
	}
	return clientCtx.PrintProto(&upgradetypes.Plan{Name: plan.Name, Height: height, Info: plan.Info})
}

// queryApplied prints the EVM block header of the block the named upgrade was
// applied at.
func queryApplied(cmd *cobra.Command, args []string) error {
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	height, err := callInto[uint64](cmd, c, upgradeprecompile.Address, upgradeprecompile.ABI, upgradeprecompile.AppliedPlanMethod, args[0])
	if err != nil {
		return err
	}
	if height == 0 {
		return errors.New("no upgrade found")
	}
	return printHeader(cmd, clientCtx, c, new(big.Int).SetUint64(height))
}

// queryBlock prints the EVM header of the block at the height argument, or of
// the latest block when there is none or it is 0.
func queryBlock(cmd *cobra.Command, args []string) error {
	var height *big.Int
	if len(args) > 0 {
		h, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("height %q: %w", args[0], err)
		}
		if h > 0 {
			height = new(big.Int).SetUint64(h)
		}
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	return printHeader(cmd, clientCtx, c, height)
}

// printHeader prints the header of the block at height, or of the latest block
// when height is nil, as the node's eth_getBlockByNumber JSON without its
// transactions.
func printHeader(cmd *cobra.Command, clientCtx client.Context, c *evmClient, height *big.Int) error {
	tag := "latest"
	if height != nil {
		tag = hexutil.EncodeBig(height)
	}
	// The node's own fields are printed rather than a decoded types.Header,
	// whose JSON recomputes the hash as Ethereum's header hash.
	var header map[string]json.RawMessage
	if err := c.ec.Client().CallContext(cmdContext(cmd), &header, "eth_getBlockByNumber", tag, false); err != nil {
		return err
	}
	if header == nil {
		if height == nil {
			return errors.New("no latest block")
		}
		return fmt.Errorf("block %s not found", height)
	}
	delete(header, "transactions")
	bz, err := json.MarshalIndent(header, "", "  ")
	if err != nil {
		return err
	}
	return clientCtx.PrintString(string(bz) + "\n")
}

// querySubspace prints a param as V2's ParamChange. key is a V2 key, whose
// value is an object of its declared fields, or a flat key.
func querySubspace(cmd *cobra.Command, args []string) error {
	subspace, key := args[0], args[1]
	keys := fieldsOf(subspace, key)
	if k, ok := declaredParam(subspace, key); ok {
		keys = []chainparams.Key{k}
	}
	if len(keys) == 0 {
		return fmt.Errorf("%s/%s is not a param of this chain; declared: %s", subspace, key, declaredKeys(subspace))
	}
	clientCtx, c, err := queryCtx(cmd)
	if err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	var scalar json.RawMessage
	for _, k := range keys {
		vals, err := c.call(cmdContext(cmd), chainparams.Address, chainparams.ABI, chainparams.ParamMethod, k.Subspace, k.Key)
		if err != nil {
			return err
		}
		v, err := formatParam(k, new(big.Int).SetBytes(vals[0].([]byte)))
		if err != nil {
			return err
		}
		if k.Key == key {
			scalar = v
			continue
		}
		_, field := v2Key(k.Key)
		fields[field] = v
	}
	value := scalar
	if value == nil {
		if value, err = json.Marshal(fields); err != nil {
			return err
		}
	}
	return clientCtx.PrintProto(&paramproposal.ParamChange{Subspace: subspace, Key: key, Value: string(value)})
}

// collectPages calls fetch from --page-key, following next keys until at least
// --limit results are collected or the last page is read, and returns the next
// key.
func collectPages(cmd *cobra.Command, fetch func(key []byte) (n int, next []byte, err error)) ([]byte, error) {
	key, err := pageKeyFlag(cmd)
	if err != nil {
		return nil, err
	}
	limit := defaultPageLimit
	if flagChanged(cmd, flags.FlagLimit) {
		if l, _ := cmd.Flags().GetUint64(flags.FlagLimit); l > 0 {
			limit = int(l) //nolint:gosec // a page limit fits int
		}
	}
	total := 0
	for {
		n, next, err := fetch(key)
		if err != nil {
			return nil, err
		}
		total += n
		if len(next) == 0 {
			return nil, nil
		}
		if total >= limit {
			return next, nil
		}
		if bytes.Equal(next, key) {
			return nil, fmt.Errorf("page key %x did not advance", key)
		}
		key = next
	}
}

// pageKeyFlag reads --page-key as the base64 next_key a paged query prints.
func pageKeyFlag(cmd *cobra.Command) ([]byte, error) {
	s, _ := cmd.Flags().GetString(flags.FlagPageKey)
	if s == "" {
		return []byte{}, nil
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("--%s: want the base64 next_key of a previous page: %w", flags.FlagPageKey, err)
	}
	return key, nil
}

// addressFlag reads an optional 0x address flag, returning the zero address
// when it is not set.
func addressFlag(cmd *cobra.Command, name string) (common.Address, error) {
	s, _ := cmd.Flags().GetString(name)
	if s == "" {
		return common.Address{}, nil
	}
	return parseAddress(s)
}

// parseAddress parses a 0x address.
func parseAddress(s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("%q is not a 0x address; --%s takes EVM addresses", s, FlagEVMRPC)
	}
	return common.HexToAddress(s), nil
}
