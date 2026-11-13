package cli

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramproposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

// baseDenom is the only denom an EVM-only chain's deposits are in.
const baseDenom = "usei"

// decPrecision is the decimal places of a 1e18-scaled ratio or weight.
const decPrecision = 18

var weiPerUsei = big.NewInt(1_000_000_000_000)

// Content types of the gov precompile's ProposalData.contentType.
const (
	contentText uint8 = iota
	contentSoftwareUpgrade
	contentCancelSoftwareUpgrade
	contentParamChange
)

// Go shapes of the gov and upgrade precompiles' ABI structs.
type (
	proposalData struct {
		Id               uint64
		Status           int32
		FinalTallyResult tallyData
		SubmitTime       uint64
		DepositEndTime   uint64
		TotalDeposit     *big.Int
		VotingStartTime  uint64
		VotingEndTime    uint64
		IsExpedited      bool
		ContentType      uint8
		Content          []byte
	}
	tallyData struct {
		Yes        *big.Int
		Abstain    *big.Int
		No         *big.Int
		NoWithVeto *big.Int
	}
	weightedOption struct {
		Option int32
		Weight *big.Int
	}
	voteData struct {
		ProposalId uint64
		Voter      common.Address
		Options    []weightedOption
	}
	depositData struct {
		ProposalId uint64
		Depositor  common.Address
		Amount     *big.Int
	}
	govParamsData struct {
		VotingPeriod          uint64
		ExpeditedVotingPeriod uint64
		MinDeposit            *big.Int
		MaxDepositPeriod      uint64
		MinExpeditedDeposit   *big.Int
		Quorum                *big.Int
		Threshold             *big.Int
		VetoThreshold         *big.Int
		ExpeditedQuorum       *big.Int
		ExpeditedThreshold    *big.Int
	}
	upgradePlan struct {
		Name   string
		Height uint64
		Info   string
	}
	paramChange struct {
		Subspace string
		Key      string
		Value    []byte
	}
	textContent struct {
		Title       string
		Description string
		IsExpedited bool
	}
	softwareUpgradeContent struct {
		Title       string
		Description string
		Plan        upgradePlan
		IsExpedited bool
	}
	cancelSoftwareUpgradeContent struct {
		Title       string
		Description string
		Name        string
		IsExpedited bool
	}
	paramChangeContent struct {
		Title       string
		Description string
		Changes     []paramChange
		IsExpedited bool
	}
)

var (
	stringArg  = abi.ArgumentMarshaling{Type: "string"}
	planArg    = abi.ArgumentMarshaling{Name: "plan", Type: "tuple", Components: []abi.ArgumentMarshaling{named("name", stringArg), {Name: "height", Type: "uint64"}, named("info", stringArg)}}
	changesArg = abi.ArgumentMarshaling{Name: "changes", Type: "tuple[]", Components: []abi.ArgumentMarshaling{named("subspace", stringArg), named("key", stringArg), {Name: "value", Type: "bytes"}}}
	expArg     = abi.ArgumentMarshaling{Name: "isExpedited", Type: "bool"}

	textArgs          = contentArgs(named("title", stringArg), named("description", stringArg), expArg)
	softwareUpgArgs   = contentArgs(named("title", stringArg), named("description", stringArg), planArg, expArg)
	cancelUpgradeArgs = contentArgs(named("title", stringArg), named("description", stringArg), named("name", stringArg), expArg)
	paramChangeArgs   = contentArgs(named("title", stringArg), named("description", stringArg), changesArg, expArg)
)

func named(name string, arg abi.ArgumentMarshaling) abi.ArgumentMarshaling {
	arg.Name = name
	return arg
}

// contentArgs are the arguments of the abi.encode of a content struct.
func contentArgs(fields ...abi.ArgumentMarshaling) abi.Arguments {
	t, err := abi.NewType("tuple", "", fields)
	if err != nil {
		panic(err)
	}
	return abi.Arguments{{Type: t}}
}

// decodeContentAs decodes bz, the abi.encode of a content struct, into T.
func decodeContentAs[T any](args abi.Arguments, bz []byte) (T, error) {
	var out T
	vals, err := args.Unpack(bz)
	if err != nil {
		return out, err
	}
	return *abi.ConvertType(vals[0], new(T)).(*T), nil
}

// v2Content returns a proposal's content as V2's proposal content type.
func v2Content(contentType uint8, bz []byte) (govtypes.Content, error) {
	switch contentType {
	case contentText:
		c, err := decodeContentAs[textContent](textArgs, bz)
		if err != nil {
			return nil, err
		}
		return &govtypes.TextProposal{Title: c.Title, Description: c.Description, IsExpedited: c.IsExpedited}, nil
	case contentSoftwareUpgrade:
		c, err := decodeContentAs[softwareUpgradeContent](softwareUpgArgs, bz)
		if err != nil {
			return nil, err
		}
		height, err := toInt64(c.Plan.Height)
		if err != nil {
			return nil, err
		}
		return &upgradetypes.SoftwareUpgradeProposal{
			Title:       c.Title,
			Description: c.Description,
			Plan:        upgradetypes.Plan{Name: c.Plan.Name, Height: height, Info: c.Plan.Info},
		}, nil
	case contentCancelSoftwareUpgrade:
		c, err := decodeContentAs[cancelSoftwareUpgradeContent](cancelUpgradeArgs, bz)
		if err != nil {
			return nil, err
		}
		return &upgradetypes.CancelSoftwareUpgradeProposal{Title: c.Title, Description: c.Description}, nil
	case contentParamChange:
		c, err := decodeContentAs[paramChangeContent](paramChangeArgs, bz)
		if err != nil {
			return nil, err
		}
		changes, err := v2ParamChanges(c.Changes)
		if err != nil {
			return nil, err
		}
		return &paramproposal.ParameterChangeProposal{
			Title:       c.Title,
			Description: c.Description,
			Changes:     changes,
			IsExpedited: c.IsExpedited,
		}, nil
	default:
		return nil, fmt.Errorf("unknown proposal content type %d", contentType)
	}
}

// v2Proposal returns p as V2's Proposal.
// cancelPlanName returns the plan a cancel-software-upgrade proposal cancels,
// and false for any other proposal.
func cancelPlanName(p proposalData) (string, bool, error) {
	if p.ContentType != contentCancelSoftwareUpgrade {
		return "", false, nil
	}
	c, err := decodeContentAs[cancelSoftwareUpgradeContent](cancelUpgradeArgs, p.Content)
	if err != nil {
		return "", false, fmt.Errorf("proposal %d: %w", p.Id, err)
	}
	return c.Name, true, nil
}

func v2Proposal(p proposalData) (govtypes.Proposal, error) {
	content, err := v2Content(p.ContentType, p.Content)
	if err != nil {
		return govtypes.Proposal{}, fmt.Errorf("proposal %d: %w", p.Id, err)
	}
	msg, ok := content.(proto.Message)
	if !ok {
		return govtypes.Proposal{}, fmt.Errorf("proposal %d: content %T is not a proto message", p.Id, content)
	}
	contentAny, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		return govtypes.Proposal{}, err
	}
	return govtypes.Proposal{
		ProposalId:       p.Id,
		Content:          contentAny,
		Status:           govtypes.ProposalStatus(p.Status),
		FinalTallyResult: v2Tally(p.FinalTallyResult),
		SubmitTime:       unixNano(p.SubmitTime),
		DepositEndTime:   unixNano(p.DepositEndTime),
		TotalDeposit:     weiToCoins(p.TotalDeposit),
		VotingStartTime:  unixNano(p.VotingStartTime),
		VotingEndTime:    unixNano(p.VotingEndTime),
		IsExpedited:      p.IsExpedited,
	}, nil
}

func v2Tally(t tallyData) govtypes.TallyResult {
	return govtypes.TallyResult{
		Yes:        sdk.NewIntFromBigInt(orZero(t.Yes)),
		Abstain:    sdk.NewIntFromBigInt(orZero(t.Abstain)),
		No:         sdk.NewIntFromBigInt(orZero(t.No)),
		NoWithVeto: sdk.NewIntFromBigInt(orZero(t.NoWithVeto)),
	}
}

// v2Vote returns v as V2's Vote, with the voter as a 0x address.
func v2Vote(v voteData) govtypes.Vote {
	options := make([]govtypes.WeightedVoteOption, len(v.Options))
	for i, o := range v.Options {
		options[i] = govtypes.WeightedVoteOption{Option: govtypes.VoteOption(o.Option), Weight: decFromScaled(o.Weight)}
	}
	out := govtypes.Vote{ProposalId: v.ProposalId, Voter: v.Voter.Hex(), Options: options}
	if len(options) == 1 {
		out.Option = options[0].Option //nolint:staticcheck // V2 still fills the deprecated single option
	}
	return out
}

// v2Deposit returns d as V2's Deposit, with the depositor as a 0x address.
func v2Deposit(d depositData) govtypes.Deposit {
	return govtypes.Deposit{ProposalId: d.ProposalId, Depositor: d.Depositor.Hex(), Amount: weiToCoins(d.Amount)}
}

// v2GovParams returns p as V2's gov Params.
func v2GovParams(p govParamsData) (govtypes.Params, error) {
	voting, err := toDuration(p.VotingPeriod)
	if err != nil {
		return govtypes.Params{}, err
	}
	expeditedVoting, err := toDuration(p.ExpeditedVotingPeriod)
	if err != nil {
		return govtypes.Params{}, err
	}
	maxDeposit, err := toDuration(p.MaxDepositPeriod)
	if err != nil {
		return govtypes.Params{}, err
	}
	return govtypes.Params{
		VotingParams: govtypes.VotingParams{VotingPeriod: voting, ExpeditedVotingPeriod: expeditedVoting},
		TallyParams: govtypes.TallyParams{
			Quorum:             decFromScaled(p.Quorum),
			Threshold:          decFromScaled(p.Threshold),
			VetoThreshold:      decFromScaled(p.VetoThreshold),
			ExpeditedQuorum:    decFromScaled(p.ExpeditedQuorum),
			ExpeditedThreshold: decFromScaled(p.ExpeditedThreshold),
		},
		DepositParams: govtypes.DepositParams{
			MinDeposit:          weiToCoins(p.MinDeposit),
			MaxDepositPeriod:    maxDeposit,
			MinExpeditedDeposit: weiToCoins(p.MinExpeditedDeposit),
		},
	}, nil
}

// depositWei returns coins, a V2 coin string of usei, in wei. An empty string
// is zero.
func depositWei(coins string) (*big.Int, error) {
	if strings.TrimSpace(coins) == "" {
		return new(big.Int), nil
	}
	parsed, err := sdk.ParseCoinsNormalized(coins)
	if err != nil {
		return nil, err
	}
	for _, c := range parsed {
		if c.Denom != baseDenom {
			return nil, fmt.Errorf("deposit %q: only %s is accepted", coins, baseDenom)
		}
	}
	usei := parsed.AmountOf(baseDenom).BigInt()
	return usei.Mul(usei, weiPerUsei), nil
}

// weiToCoins returns wei as usei coins, dropping any fraction of a usei.
func weiToCoins(wei *big.Int) sdk.Coins {
	usei := new(big.Int).Quo(orZero(wei), weiPerUsei)
	if usei.Sign() == 0 {
		return sdk.Coins{}
	}
	return sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.NewIntFromBigInt(usei)))
}

// decFromScaled returns a 1e18-scaled integer as a Dec.
func decFromScaled(v *big.Int) sdk.Dec {
	return sdk.NewDecFromBigIntWithPrec(orZero(v), decPrecision)
}

// scaledFromDec returns d as a 1e18-scaled integer.
func scaledFromDec(d sdk.Dec) *big.Int {
	return d.BigInt()
}

// unixNano returns a Unix nanosecond time, with 0 as the zero time.
func unixNano(ns uint64) time.Time {
	if ns == 0 || ns > math.MaxInt64 {
		return time.Time{}
	}
	return time.Unix(0, int64(ns)).UTC()
}

func toDuration(ns uint64) (time.Duration, error) {
	if ns > math.MaxInt64 {
		return 0, fmt.Errorf("duration %dns overflows", ns)
	}
	return time.Duration(ns), nil
}

func toInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%d overflows int64", v)
	}
	return int64(v), nil
}

func orZero(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return v
}
