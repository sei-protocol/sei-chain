// Package gov is the EVM-only governance precompile. Its ABI is Gov.sol and its
// state is docs/giga-storage/GovStorage.sol, held as contract storage at
// Address.
//
// Unlike x/gov, it accepts at most 200 depositors per proposal and requires a
// positive minimum deposit.
package gov

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
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
	VoteMethod               = "vote"
	VoteWeightedMethod       = "voteWeighted"
	DepositMethod            = "deposit"
	SubmitTextProposalMethod = "submitTextProposal"
	ProposalMethod           = "proposal"
	ProposalsMethod          = "proposals"
	GetVoteMethod            = "getVote"
	VotesMethod              = "votes"
	ParamsMethod             = "params"
	GetDepositMethod         = "getDeposit"
	DepositsMethod           = "deposits"
	TallyResultMethod        = "tallyResult"
)

// Events.
const (
	SubmitProposalEvent  = "SubmitProposal"
	ProposalDepositEvent = "ProposalDeposit"
	ProposalVoteEvent    = "ProposalVote"
)

var (
	errUnsupportedUpgrade = errors.New("software upgrade proposals are not supported yet")
	errAuthzUnsupported   = errors.New("gov authorizations are not supported")
)

// retiredMethods are the methods, by signature, gov keeps in its ABI only to
// revert with an error naming the call to use instead.
var retiredMethods = map[string]error{
	"submitProposal(string)":                                                  errors.New("submitProposal(string) is not supported; use submitTextProposal"),
	"voteWeighted(uint64,(int32,string)[])":                                   errors.New("voteWeighted with decimal string weights is not supported; use voteWeighted(uint64,(int32,uint256)[]) with 1e18-scaled weights"),
	"submitSoftwareUpgradeProposal(string,string,bool,(string,int64,string))": errUnsupportedUpgrade,
	"submitCancelSoftwareUpgradeProposal(string,string,bool)":                 errUnsupportedUpgrade,
	"grantVoteAuthorization(address,int64)":                                   errAuthzUnsupported,
	"grantProposalAuthorization(address,int64)":                               errAuthzUnsupported,
	"revokeVoteAuthorization(address)":                                        errAuthzUnsupported,
	"revokeProposalAuthorization(address)":                                    errAuthzUnsupported,
	"voteWithAuthorization(address,uint64,int32)":                             errAuthzUnsupported,
	"submitProposalWithAuthorization(address,string)":                         errAuthzUnsupported,
}

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
	if method.IsConstant() {
		return queryGas + queryByteGas*args
	}
	return transactionGas + transactionByteGas*args
}

func (p Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if ctx.DelegateCall {
		return nil, errDelegateCall
	}
	method, err := methodOf(input)
	if err != nil {
		return nil, err
	}
	if err := retiredMethods[method.Sig]; err != nil {
		return nil, err
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	if !method.IsPayable() && ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}
	s := newStore(ctx.State)

	switch method.RawName {
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

	switch method.RawName {
	case VoteMethod:
		return p.vote(ctx, s, method, args)
	case VoteWeightedMethod:
		return p.voteWeighted(ctx, s, method, args)
	case DepositMethod:
		return p.deposit(ctx, s, method, args)
	case SubmitTextProposalMethod:
		return p.submitTextProposal(ctx, s, method, args)
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
func SetParams(state precompiles.State, p Params) {
	newStore(state).setParams(p)
}

// HasParams reports whether gov's params have been written.
func HasParams(state precompiles.State) bool {
	return newStore(state).params().VotingPeriod != 0
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
