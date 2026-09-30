// Package gov is the EVM-only chain's governance precompile. It carries Cosmos
// x/gov software-upgrade proposals in EVM transactions: it keeps the selectors
// and return types of precompiles/gov for the methods it implements, and the
// tally rules of x/gov, with voting weight taken from a fixed voter set.
package gov

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Address is where the governance precompile is installed, the address of
// precompiles/gov.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001006")

//go:embed abi.json
var abiJSON []byte

// ABI is the precompile's interface, a subset of precompiles/gov's.
var ABI = mustParseABI()

func mustParseABI() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}

// Proposal statuses, the values of cosmos.gov.v1beta1.ProposalStatus.
const (
	StatusVotingPeriod int32 = 2
	StatusPassed       int32 = 3
	StatusRejected     int32 = 4
	StatusFailed       int32 = 5
)

// Vote options, the values of cosmos.gov.v1beta1.VoteOption.
const (
	OptionYes        int32 = 1
	OptionAbstain    int32 = 2
	OptionNo         int32 = 3
	OptionNoWithVeto int32 = 4
)

// Proposal types accepted by submitProposal, as in precompiles/gov.
const (
	ProposalTypeSoftwareUpgrade       = "SoftwareUpgrade"
	ProposalTypeCancelSoftwareUpgrade = "CancelSoftwareUpgrade"
)

// Content limits. Title and description match x/gov's.
const (
	MaxTitleLength        = 140
	MaxDescriptionLength  = 10_000
	MaxPlanNameLength     = 140
	MaxPlanInfoLength     = 10_000
	maxProposalJSONLength = 32 * 1024
)

// maxProposalsEndedPerBlock bounds the proposals one block's EndBlock tallies.
// Later ones end in following blocks.
const maxProposalsEndedPerBlock = 16

// Gas charged per call, before the call runs.
const (
	gasPerSlotRead   = 2_100
	gasPerSlotWrite  = 22_100
	gasQueryBase     = 10_000
	gasUnknownMethod = 3_000
)

var (
	ErrUnknownMethod           = errors.New("unknown method")
	ErrDelegateCall            = errors.New("governance precompile cannot be delegate-called")
	ErrReadOnly                = errors.New("cannot write in a static call")
	ErrValueNotAccepted        = errors.New("governance precompile does not accept value")
	ErrNotVoter                = errors.New("caller is not a voter")
	ErrInvalidArguments        = errors.New("invalid arguments")
	ErrInvalidProposal         = errors.New("invalid proposal")
	ErrUnsupportedProposalType = errors.New("unsupported proposal type")
	ErrProposalNotFound        = errors.New("proposal not found")
	ErrProposalNotInVoting     = errors.New("proposal is not in its voting period")
	ErrInvalidVoteOption       = errors.New("invalid vote option")
	ErrVoteNotFound            = errors.New("vote not found")
)

// Contract is the governance precompile.
type Contract struct {
	addr    common.Address
	params  Params
	voters  []Voter
	weights map[common.Address]uint64
	total   uint64
}

var (
	_ precompiles.Contract   = (*Contract)(nil)
	_ precompiles.EndBlocker = (*Contract)(nil)
)

// New returns the governance precompile for genesis, installed at Address.
func New(genesis Genesis) (*Contract, error) {
	if err := genesis.Validate(); err != nil {
		return nil, err
	}
	c := &Contract{
		addr:    Address,
		params:  genesis.Params,
		voters:  sortedVoters(genesis.Voters),
		weights: make(map[common.Address]uint64, len(genesis.Voters)),
	}
	for _, voter := range c.voters {
		c.weights[voter.Address] = voter.Weight
		c.total += voter.Weight
	}
	return c, nil
}

// Registry returns a precompiles.Registry holding only c.
func (c *Contract) Registry() precompiles.Registry {
	return precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{c.addr: c})
}

func (c *Contract) RequiredGas(input []byte) uint64 {
	method, err := methodOf(input)
	if err != nil {
		return gasUnknownMethod
	}
	voters := uint64(len(c.voters))
	switch method.Name {
	case "submitProposal":
		// The queue entry, the counters, the fixed fields, and the strings copied out of input.
		return gasQueryBase + (16+uint64(len(input)+31)/32)*gasPerSlotWrite
	case "vote":
		return gasQueryBase + 4*gasPerSlotRead + gasPerSlotWrite
	case "getVote", "params":
		return gasQueryBase + 2*gasPerSlotRead
	case "tallyResult":
		return gasQueryBase + (voters+8)*gasPerSlotRead
	case "proposal":
		return gasQueryBase + maxProposalSlots*gasPerSlotRead
	}
	return gasUnknownMethod
}

// maxProposalSlots is the number of slots the largest proposal occupies.
var maxProposalSlots = 16 + stringSlots(MaxTitleLength) + stringSlots(MaxDescriptionLength) +
	stringSlots(MaxPlanNameLength) + stringSlots(MaxPlanInfoLength)

func methodOf(input []byte) (*abi.Method, error) {
	if len(input) < 4 {
		return nil, ErrUnknownMethod
	}
	method, err := ABI.MethodById(input[:4])
	if err != nil {
		return nil, ErrUnknownMethod
	}
	return method, nil
}

// Run executes one call. Every failure reverts with an Error(string) reason and
// leaves the unused gas with the caller.
func (c *Contract) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	method, err := methodOf(input)
	if err != nil {
		return revert(err)
	}
	if ctx.DelegateCall {
		return revert(ErrDelegateCall)
	}
	if ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return revert(ErrValueNotAccepted)
	}
	if !method.IsConstant() && ctx.ReadOnly {
		return revert(ErrReadOnly)
	}
	args, err := unpackArgs(method, input[4:])
	if err != nil {
		return revert(err)
	}
	var result any
	switch method.Name {
	case "submitProposal":
		raw, argErr := arg[string](args, 0)
		if argErr != nil {
			return revert(argErr)
		}
		result, err = c.submitProposal(ctx, raw)
	case "vote":
		id, idErr := arg[uint64](args, 0)
		option, optionErr := arg[int32](args, 1)
		if argErr := errors.Join(idErr, optionErr); argErr != nil {
			return revert(argErr)
		}
		result, err = c.vote(ctx, id, option)
	case "getVote":
		id, idErr := arg[uint64](args, 0)
		voter, voterErr := arg[common.Address](args, 1)
		if argErr := errors.Join(idErr, voterErr); argErr != nil {
			return revert(argErr)
		}
		result, err = c.getVote(ctx, id, voter)
	case "proposal":
		id, argErr := arg[uint64](args, 0)
		if argErr != nil {
			return revert(argErr)
		}
		result, err = c.proposal(ctx, id)
	case "tallyResult":
		id, argErr := arg[uint64](args, 0)
		if argErr != nil {
			return revert(argErr)
		}
		result, err = c.tallyResult(ctx, id)
	case "params":
		result = c.paramsData()
	default:
		return revert(ErrUnknownMethod)
	}
	if err != nil {
		return revert(err)
	}
	output, err := method.Outputs.Pack(result)
	if err != nil {
		return revert(err)
	}
	return output, nil
}

// unpackArgs returns method's arguments from input. It refuses input that is
// not exactly the ABI encoding of method's arguments, such as input with
// trailing bytes, dirty padding or out-of-range integers.
func unpackArgs(method *abi.Method, input []byte) ([]any, error) {
	args, err := method.Inputs.Unpack(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	if len(args) != len(method.Inputs) {
		return nil, fmt.Errorf("%w: %s takes %d arguments, got %d", ErrInvalidArguments, method.Name, len(method.Inputs), len(args))
	}
	canonical, err := method.Inputs.Pack(args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	if !bytes.Equal(canonical, input) {
		return nil, fmt.Errorf("%w: not the canonical encoding", ErrInvalidArguments)
	}
	return args, nil
}

// arg returns args[i] as a T.
func arg[T any](args []any, i int) (T, error) {
	var zero T
	if i >= len(args) {
		return zero, fmt.Errorf("%w: missing argument %d", ErrInvalidArguments, i)
	}
	v, ok := args[i].(T)
	if !ok {
		return zero, fmt.Errorf("%w: argument %d is %T, not %T", ErrInvalidArguments, i, args[i], zero)
	}
	return v, nil
}

func revert(err error) ([]byte, error) {
	return revertReason(err), vm.ErrExecutionReverted
}

var revertSelector = crypto.Keccak256([]byte("Error(string)"))[:4]

func revertReason(err error) []byte {
	stringType, _ := abi.NewType("string", "", nil)
	packed, packErr := abi.Arguments{{Type: stringType}}.Pack(err.Error())
	if packErr != nil {
		return nil
	}
	return append(bytes.Clone(revertSelector), packed...)
}

// proposalJSON is the submitProposal argument, the format precompiles/gov parses.
type proposalJSON struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	IsExpedited bool   `json:"is_expedited,omitempty"`
	Plan        *struct {
		Name   string `json:"name"`
		Height int64  `json:"height"`
		Info   string `json:"info,omitempty"`
	} `json:"plan,omitempty"`
}

func (c *Contract) submitProposal(ctx *precompiles.Context, raw string) (uint64, error) {
	if _, ok := c.weights[ctx.Caller]; !ok {
		return 0, ErrNotVoter
	}
	p, err := parseProposal(raw)
	if err != nil {
		return 0, err
	}
	w := newWriter(c.addr, ctx.State)
	id := w.u64(slotProposalCount) + 1
	w.setU64(slotProposalCount, id)
	tail := w.u64(slotQueueTail)
	w.setU64(queueSlot(tail), id)
	w.setU64(slotQueueTail, tail+1)

	w.setU64(proposalSlot(id, fieldKind), p.kind)
	w.setU64(proposalSlot(id, fieldStatus), uint64(StatusVotingPeriod))
	w.setAddress(proposalSlot(id, fieldProposer), ctx.Caller)
	w.setU64(proposalSlot(id, fieldSubmitTime), ctx.Block.Time)
	w.setU64(proposalSlot(id, fieldVotingEndTime), ctx.Block.Time+c.params.VotingPeriod)
	w.setStr(proposalSlot(id, fieldTitle), p.title)
	w.setStr(proposalSlot(id, fieldDescription), p.description)
	if p.kind == kindSoftwareUpgrade {
		w.setU64(proposalSlot(id, fieldPlanHeight), p.plan.Height)
		w.setStr(proposalSlot(id, fieldPlanName), p.plan.Name)
		w.setStr(proposalSlot(id, fieldPlanInfo), p.plan.Info)
	}
	return id, nil
}

// parsedProposal is a validated submitProposal argument.
type parsedProposal struct {
	kind        uint64
	title       string
	description string
	plan        Plan
}

// parseProposal applies the checks x/gov and x/upgrade apply at submission.
func parseProposal(raw string) (parsedProposal, error) {
	if len(raw) > maxProposalJSONLength {
		return parsedProposal{}, fmt.Errorf("%w: longer than %d bytes", ErrInvalidProposal, maxProposalJSONLength)
	}
	var p proposalJSON
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return parsedProposal{}, fmt.Errorf("%w: %w", ErrInvalidProposal, err)
	}
	if p.Title == "" || len(p.Title) > MaxTitleLength {
		return parsedProposal{}, fmt.Errorf("%w: title must be 1 to %d bytes", ErrInvalidProposal, MaxTitleLength)
	}
	if p.Description == "" || len(p.Description) > MaxDescriptionLength {
		return parsedProposal{}, fmt.Errorf("%w: description must be 1 to %d bytes", ErrInvalidProposal, MaxDescriptionLength)
	}
	if p.IsExpedited {
		return parsedProposal{}, fmt.Errorf("%w: expedited proposals", ErrUnsupportedProposalType)
	}
	parsed := parsedProposal{title: p.Title, description: p.Description}
	switch p.Type {
	case ProposalTypeSoftwareUpgrade:
		if p.Plan == nil {
			return parsedProposal{}, fmt.Errorf("%w: upgrade plan must be specified", ErrInvalidProposal)
		}
		if p.Plan.Height <= 0 {
			return parsedProposal{}, fmt.Errorf("%w: upgrade height must be positive", ErrInvalidProposal)
		}
		if p.Plan.Name == "" || len(p.Plan.Name) > MaxPlanNameLength {
			return parsedProposal{}, fmt.Errorf("%w: upgrade name must be 1 to %d bytes", ErrInvalidProposal, MaxPlanNameLength)
		}
		if len(p.Plan.Info) > MaxPlanInfoLength {
			return parsedProposal{}, fmt.Errorf("%w: upgrade info longer than %d bytes", ErrInvalidProposal, MaxPlanInfoLength)
		}
		parsed.kind = kindSoftwareUpgrade
		parsed.plan = Plan{Name: p.Plan.Name, Height: uint64(p.Plan.Height), Info: p.Plan.Info}
	case ProposalTypeCancelSoftwareUpgrade:
		parsed.kind = kindCancelSoftwareUpgrade
	default:
		return parsedProposal{}, fmt.Errorf("%w: %q", ErrUnsupportedProposalType, p.Type)
	}
	return parsed, nil
}

func (c *Contract) vote(ctx *precompiles.Context, id uint64, option int32) (bool, error) {
	if _, ok := c.weights[ctx.Caller]; !ok {
		return false, ErrNotVoter
	}
	if option < OptionYes || option > OptionNoWithVeto {
		return false, ErrInvalidVoteOption
	}
	w := newWriter(c.addr, ctx.State)
	if !w.exists(id) {
		return false, ErrProposalNotFound
	}
	if w.status(id) != StatusVotingPeriod || ctx.Block.Time >= w.u64(proposalSlot(id, fieldVotingEndTime)) {
		return false, ErrProposalNotInVoting
	}
	w.setU64(voteSlot(id, ctx.Caller), uint64(option)) //nolint:gosec // option is in [1, 4].
	return true, nil
}

// weightOne is the weight of a whole vote, formatted as sdk.Dec.
var weightOne = DecOne.String()

// voteData mirrors precompiles/gov's VoteData tuple.
type voteData struct {
	ProposalId uint64 //nolint:revive // must match abi component name "proposalId"
	Voter      string
	Options    []weightedVoteOptionData
}

type weightedVoteOptionData struct {
	Option int32
	Weight string
}

func (c *Contract) getVote(ctx *precompiles.Context, id uint64, voter common.Address) (voteData, error) {
	s := store{addr: c.addr, db: ctx.State}
	option := s.u64(voteSlot(id, voter))
	if option == 0 {
		return voteData{}, ErrVoteNotFound
	}
	return voteData{
		ProposalId: id,
		Voter:      voter.Hex(),
		Options:    []weightedVoteOptionData{{Option: int32(option), Weight: weightOne}}, //nolint:gosec // stored options are in [1, 4].
	}, nil
}

// coin mirrors precompiles/gov's Coin tuple.
type coin struct {
	Amount *big.Int
	Denom  string
}

// tallyResultData mirrors precompiles/gov's TallyResultData tuple.
type tallyResultData struct {
	Yes        string
	Abstain    string
	No         string
	NoWithVeto string
}

// proposalData mirrors precompiles/gov's ProposalData tuple.
type proposalData struct {
	Id               uint64 //nolint:revive // must match abi component name "id"
	Status           int32
	FinalTallyResult tallyResultData
	SubmitTime       int64
	DepositEndTime   int64
	TotalDeposit     []coin
	VotingStartTime  int64
	VotingEndTime    int64
	IsExpedited      bool
	Content          []byte
}

func (c *Contract) proposal(ctx *precompiles.Context, id uint64) (proposalData, error) {
	s := store{addr: c.addr, db: ctx.State}
	if !s.exists(id) {
		return proposalData{}, ErrProposalNotFound
	}
	content, err := json.Marshal(s.content(id))
	if err != nil {
		return proposalData{}, err
	}
	submit := int64(s.u64(proposalSlot(id, fieldSubmitTime))) //nolint:gosec // block times fit in int64.
	end := int64(s.u64(proposalSlot(id, fieldVotingEndTime))) //nolint:gosec // block times fit in int64.
	return proposalData{
		Id:               id,
		Status:           s.status(id),
		FinalTallyResult: s.finalTally(id).data(),
		SubmitTime:       submit,
		DepositEndTime:   submit,
		TotalDeposit:     []coin{},
		VotingStartTime:  submit,
		VotingEndTime:    end,
		Content:          content,
	}, nil
}

func (c *Contract) tallyResult(ctx *precompiles.Context, id uint64) (tallyResultData, error) {
	s := store{addr: c.addr, db: ctx.State}
	if !s.exists(id) {
		return tallyResultData{}, ErrProposalNotFound
	}
	if s.status(id) == StatusVotingPeriod {
		return c.tally(s, id).data(), nil
	}
	return s.finalTally(id).data(), nil
}

// govParams mirrors precompiles/gov's GovParams tuple.
type govParams struct {
	VotingPeriod          uint64
	ExpeditedVotingPeriod uint64
	MinDeposit            []coin
	MaxDepositPeriod      uint64
	MinExpeditedDeposit   []coin
	Quorum                string
	Threshold             string
	VetoThreshold         string
	ExpeditedQuorum       string
	ExpeditedThreshold    string
}

func (c *Contract) paramsData() govParams {
	return govParams{
		VotingPeriod:          c.params.VotingPeriod,
		ExpeditedVotingPeriod: c.params.VotingPeriod,
		MinDeposit:            []coin{},
		MinExpeditedDeposit:   []coin{},
		Quorum:                c.params.Quorum.String(),
		Threshold:             c.params.Threshold.String(),
		VetoThreshold:         c.params.VetoThreshold.String(),
		ExpeditedQuorum:       c.params.Quorum.String(),
		ExpeditedThreshold:    c.params.Threshold.String(),
	}
}

func (s store) exists(id uint64) bool {
	return id != 0 && id <= s.u64(slotProposalCount)
}

func (s store) status(id uint64) int32 {
	return int32(s.u64(proposalSlot(id, fieldStatus))) //nolint:gosec // stored statuses are small.
}

// proposalContent is a proposal's content as precompiles/gov returns it, the
// JSON of its x/upgrade proposal.
type proposalContent struct {
	Type        string       `json:"@type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Plan        *contentPlan `json:"plan,omitempty"`
}

type contentPlan struct {
	Name   string `json:"name"`
	Height string `json:"height"`
	Info   string `json:"info"`
}

func (s store) content(id uint64) proposalContent {
	content := proposalContent{
		Type:        "/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal",
		Title:       s.str(proposalSlot(id, fieldTitle)),
		Description: s.str(proposalSlot(id, fieldDescription)),
	}
	if s.u64(proposalSlot(id, fieldKind)) == kindSoftwareUpgrade {
		plan := s.proposalPlan(id)
		content.Type = "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal"
		content.Plan = &contentPlan{Name: plan.Name, Height: fmt.Sprint(plan.Height), Info: plan.Info}
	}
	return content
}

func (s store) proposalPlan(id uint64) Plan {
	return Plan{
		Name:   s.str(proposalSlot(id, fieldPlanName)),
		Height: s.u64(proposalSlot(id, fieldPlanHeight)),
		Info:   s.str(proposalSlot(id, fieldPlanInfo)),
	}
}
