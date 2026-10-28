package gov

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// Proposal statuses, numbered as x/gov's ProposalStatus.
const (
	StatusDepositPeriod int32 = 1
	StatusVotingPeriod  int32 = 2
	StatusPassed        int32 = 3
	StatusRejected      int32 = 4
	StatusFailed        int32 = 5
)

// Vote options, numbered as x/gov's VoteOption.
const (
	OptionYes        int32 = 1
	OptionAbstain    int32 = 2
	OptionNo         int32 = 3
	OptionNoWithVeto int32 = 4
)

const maxVoteOptions = 4

var (
	errUnknownProposal   = errors.New("unknown proposal")
	errInactiveProposal  = errors.New("inactive proposal")
	errInvalidVote       = errors.New("invalid vote option")
	errZeroDeposit       = errors.New("set `value` field to non-zero to deposit fund")
	errWeiRemainder      = errors.New("selected precompile function does not allow payment with non-zero wei remainder")
	errTooManyDepositors = errors.New("proposal has the most depositors allowed")
	errDepositTooSmall   = errors.New("deposit is too small")
)

// maxDepositors is the most distinct depositors a proposal may have.
const maxDepositors = 200

// depositorShare is the least a depositor's first deposit may be: the minimum
// deposit split across maxDepositors, rounded up to whole usei, so a proposal
// with every depositor slot taken has reached its minimum deposit.
func depositorShare(params Params, expedited bool) *big.Int {
	usei := new(big.Int).Quo(minDeposit(params, expedited), weiPerUsei)
	usei.Add(usei, big.NewInt(maxDepositors-1)).Quo(usei, big.NewInt(maxDepositors))
	return usei.Mul(usei, weiPerUsei)
}

func (p Precompile) vote(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	option := args[1].(int32)
	if !validOption(option) {
		return nil, fmt.Errorf("%w: %d", errInvalidVote, option)
	}
	if err := castVote(ctx, s, id, []weightedVote{{option: option, weight: decOne}}); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) voteWeighted(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	raw := args[1].([]struct {
		Option int32  `json:"option"`
		Weight string `json:"weight"`
	})
	if len(raw) == 0 || len(raw) > maxVoteOptions {
		return nil, fmt.Errorf("%w: between 1 and %d options required", errInvalidVote, maxVoteOptions)
	}
	options := make([]weightedVote, len(raw))
	seen := make(map[int32]bool, len(raw))
	var total uint64
	for i, o := range raw {
		if !validOption(o.Option) || seen[o.Option] {
			return nil, fmt.Errorf("%w: %d", errInvalidVote, o.Option)
		}
		seen[o.Option] = true
		weight, err := parseDec(o.Weight)
		if err != nil {
			return nil, fmt.Errorf("invalid weight format: %w", err)
		}
		if weight == 0 || weight > decOne {
			return nil, fmt.Errorf("%w: weight %s", errInvalidVote, o.Weight)
		}
		total += weight
		options[i] = weightedVote{option: o.Option, weight: weight}
	}
	if total != decOne {
		return nil, fmt.Errorf("%w: weights sum to %s, not 1", errInvalidVote, formatDec(total))
	}
	if err := castVote(ctx, s, id, options); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

// castVote records options as the caller's vote on proposal id, replacing any
// earlier vote.
func castVote(ctx *precompiles.Context, s store, id uint64, options []weightedVote) error {
	prop, ok := s.proposal(id)
	if !ok {
		return fmt.Errorf("%w: %d", errUnknownProposal, id)
	}
	if !votingOpen(s, id, prop, ctx.Block.UnixNano) {
		return fmt.Errorf("%w: %d", errInactiveProposal, id)
	}
	s.setVote(id, ctx.Caller, options)
	s.votersByProposal(id).Add(solstorage.AddressKey(ctx.Caller))
	s.proposalsVotedBy(ctx.Caller).Add(solstorage.Slot(id))
	return nil
}

func (p Precompile) deposit(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	amount, err := depositAmount(ctx.ApparentValue)
	if err != nil {
		return nil, err
	}
	if amount.Sign() == 0 {
		return nil, errZeroDeposit
	}
	prop, ok := s.proposal(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", errUnknownProposal, id)
	}
	if err := addDeposit(ctx, s, id, prop, amount); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) submitProposal(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	content, submitted, err := parseProposal(args[0].(string))
	if err != nil {
		return nil, err
	}
	amount, err := depositAmount(ctx.ApparentValue)
	if err != nil {
		return nil, err
	}

	id := s.nextProposalID()
	prop := proposal{
		status:          StatusDepositPeriod,
		isExpedited:     submitted.IsExpedited,
		submitTime:      ctx.Block.UnixNano,
		depositEndTime:  ctx.Block.UnixNano + s.params().MaxDepositPeriod,
		totalDeposit:    new(big.Int),
		tallyYes:        new(big.Int),
		tallyAbstain:    new(big.Int),
		tallyNo:         new(big.Int),
		tallyNoWithVeto: new(big.Int),
	}
	s.setProposal(id, prop)
	contentType, encoded := encodeContent(content)
	s.setProposalContent(id, contentType, encoded)
	s.depositQueue().add(id, prop.depositEndTime)

	if amount.Sign() > 0 {
		if err := addDeposit(ctx, s, id, prop, amount); err != nil {
			return nil, err
		}
	}
	return method.Outputs.Pack(id)
}

// addDeposit moves amount from the precompile's balance into escrow, credits it
// to the caller on proposal id, and opens voting once the minimum is reached.
func addDeposit(ctx *precompiles.Context, s store, id uint64, prop proposal, amount *big.Int) error {
	switch {
	case prop.status == StatusDepositPeriod && ctx.Block.UnixNano <= prop.depositEndTime:
	case votingOpen(s, id, prop, ctx.Block.UnixNano):
	default:
		return fmt.Errorf("%w: %d", errInactiveProposal, id)
	}
	depositors := s.depositors(id)
	if !depositors.Contains(solstorage.AddressKey(ctx.Caller)) {
		if depositors.Len() >= maxDepositors {
			return fmt.Errorf("%w: %d", errTooManyDepositors, id)
		}
		if share := depositorShare(s.params(), prop.isExpedited); amount.Cmp(share) < 0 {
			return fmt.Errorf("%w: a first deposit must be at least %s wei", errDepositTooSmall, share)
		}
	}
	if err := ctx.State.SubBalance(ctx.Address, amount); err != nil {
		return err
	}
	ctx.State.AddBalance(DepositEscrow, amount)

	prop.totalDeposit = new(big.Int).Add(prop.totalDeposit, amount)
	if prop.status == StatusDepositPeriod && prop.totalDeposit.Cmp(minDeposit(s.params(), prop.isExpedited)) >= 0 {
		activateVotingPeriod(ctx, s, id, &prop)
	}
	s.setProposal(id, prop)

	s.setDeposit(id, ctx.Caller, new(big.Int).Add(s.deposit(id, ctx.Caller), amount))
	depositors.Add(solstorage.AddressKey(ctx.Caller))
	s.proposalsDepositedBy(ctx.Caller).Add(solstorage.Slot(id))
	return nil
}

func activateVotingPeriod(ctx *precompiles.Context, s store, id uint64, prop *proposal) {
	params := s.params()
	period := params.VotingPeriod
	if prop.isExpedited {
		period = params.ExpeditedVotingPeriod
	}
	prop.status = StatusVotingPeriod
	prop.votingStartTime = ctx.Block.UnixNano
	prop.votingEndTime = ctx.Block.UnixNano + period
	s.depositQueue().remove(id, prop.depositEndTime)
	s.votingQueue().add(id, prop.votingEndTime)
}

// votingOpen reports whether proposal id takes votes and deposits for voting at now.
func votingOpen(s store, id uint64, prop proposal, now uint64) bool {
	return prop.status == StatusVotingPeriod && now <= prop.votingEndTime && !s.tallyCounting(id)
}

func minDeposit(params Params, expedited bool) *big.Int {
	if expedited {
		return params.MinExpeditedDeposit
	}
	return params.MinDeposit
}

// depositAmount returns value in wei, rejecting amounts that are not whole usei.
func depositAmount(value *big.Int) (*big.Int, error) {
	if value == nil {
		return new(big.Int), nil
	}
	if new(big.Int).Mod(value, weiPerUsei).Sign() != 0 {
		return nil, fmt.Errorf("%w: received %s", errWeiRemainder, value)
	}
	return value, nil
}

func validOption(option int32) bool {
	return option >= OptionYes && option <= OptionNoWithVeto
}
