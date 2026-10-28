// Package gov is the EVM-only governance precompile. Its state is
// docs/giga-storage/GovStorage.sol, held as contract storage at Address.
//
// Its ABI is precompiles/gov's without the grant*, revoke* and
// *WithAuthorization methods, and with submitParamChangeProposal. Its params
// are the params precompile's gov subspace.
//
// Unlike x/gov, it accepts at most 200 depositors per proposal and requires a
// positive minimum deposit. Unlike V2's precompile, it rejects proposal JSON
// with a field its type does not read, a field in other case, or a field given
// twice.
package gov

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

//go:embed abi.json
var abiJSON []byte

// ABI is the gov precompile's ABI.
var ABI = func() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}()

const (
	VoteMethod           = "vote"
	VoteWeightedMethod   = "voteWeighted"
	DepositMethod        = "deposit"
	SubmitProposalMethod = "submitProposal"
	ProposalMethod       = "proposal"
	ProposalsMethod      = "proposals"
	GetVoteMethod        = "getVote"
	VotesMethod          = "votes"
	ParamsMethod         = "params"
	GetDepositMethod     = "getDeposit"
	DepositsMethod       = "deposits"
	TallyResultMethod    = "tallyResult"

	// SubmitParamChangeProposalMethod is not in V2's ABI.
	SubmitParamChangeProposalMethod = "submitParamChangeProposal"
)

// Flat per-call gas, as V2's pcommon.DefaultGasCost prices it from the
// Cosmos KV gas config, charged on the arguments after the method selector.
const (
	unknownMethodGas   = 3_000
	transactionGas     = 2_000
	transactionByteGas = 30
	queryGas           = 1_000
	queryByteGas       = 3
)

var (
	errDelegateCall   = errors.New("cannot delegatecall gov")
	errStaticCall     = fmt.Errorf("cannot call gov precompile from staticcall: %w", vm.ErrWriteProtection)
	errNonPayable     = errors.New("sending funds to a non-payable function")
	errUnknownMethod  = errors.New("unknown gov method")
	errProposalsExist = errors.New("gov already has proposals")
)

// Precompile is the gov precompile. Electorate weighs votes when a voting
// period ends; with none, every tally finds nothing bonded and rejects.
type Precompile struct {
	Electorate Electorate
}

var _ precompiles.Contract = Precompile{}

func (Precompile) RequiredGas(input []byte) uint64 {
	method, err := methodOf(input)
	if err != nil {
		return unknownMethodGas
	}
	args := uint64(len(input) - 4) //nolint:gosec // methodOf requires a 4-byte selector
	switch method.Name {
	case VoteMethod, VoteWeightedMethod, DepositMethod, SubmitProposalMethod, SubmitParamChangeProposalMethod:
		return transactionGas + transactionByteGas*args
	default:
		return queryGas + queryByteGas*args
	}
}

func (p Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if ctx.DelegateCall {
		return nil, errDelegateCall
	}
	method, err := methodOf(input)
	if err != nil {
		return nil, err
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	payable := method.Name == DepositMethod || method.Name == SubmitProposalMethod || method.Name == SubmitParamChangeProposalMethod
	if !payable && ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}
	s := newStore(ctx.State)

	switch method.Name {
	case ProposalMethod:
		return p.proposalQuery(s, method, args)
	case ProposalsMethod:
		return p.proposalsQuery(s, method, args)
	case GetVoteMethod:
		return p.voteQuery(s, method, args)
	case VotesMethod:
		return p.votesQuery(s, method, args)
	case ParamsMethod:
		return p.paramsQuery(s, method)
	case GetDepositMethod:
		return p.depositQuery(s, method, args)
	case DepositsMethod:
		return p.depositsQuery(s, method, args)
	case TallyResultMethod:
		return p.tallyResultQuery(s, method, args)
	}

	if ctx.ReadOnly {
		return nil, errStaticCall
	}

	switch method.Name {
	case VoteMethod:
		return p.vote(ctx, s, method, args)
	case VoteWeightedMethod:
		return p.voteWeighted(ctx, s, method, args)
	case DepositMethod:
		return p.deposit(ctx, s, method, args)
	case SubmitProposalMethod:
		return p.submitProposal(ctx, s, method, args)
	case SubmitParamChangeProposalMethod:
		return p.submitParamChangeProposal(ctx, s, method, args, input)
	}
	return nil, errUnknownMethod
}

func methodOf(input []byte) (*abi.Method, error) {
	if len(input) < 4 {
		return nil, errUnknownMethod
	}
	method, err := ABI.MethodById(input[:4])
	if err != nil {
		return nil, errUnknownMethod
	}
	return method, nil
}

// SetParams writes p as gov's params, for genesis.
func SetParams(state precompiles.State, p Params) error {
	stored, err := p.stored()
	if err != nil {
		return err
	}
	return chainparams.WriteGov(state, stored)
}

// HasParams reports whether gov's params have been written.
func HasParams(state precompiles.State) bool {
	return chainparams.ReadGov(state).VotingPeriod != 0
}

// SetStartingProposalID sets the id gov assigns its first proposal, for
// genesis. Ids below it are never assigned. It fails once any proposal has an
// id.
func SetStartingProposalID(state precompiles.State, id uint64) error {
	s := newStore(state)
	if s.lastProposalID() != 0 {
		return errProposalsExist
	}
	s.setStartingProposalID(id)
	return nil
}
