package gov

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var (
	errVoteNotFound    = errors.New("vote not found")
	errDepositNotFound = errors.New("deposit not found")
)

func (p Precompile) proposalQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	prop, ok := s.proposal(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", errUnknownProposal, id)
	}
	return method.Outputs.Pack(proposalData(s, id, prop))
}

// proposalsQuery pages through proposal ids in order, keeping those that match
// the filters, so a page may hold fewer than pageLimit proposals while still
// returning a next key.
func (p Precompile) proposalsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	status := args[0].(int32)
	voter := args[1].(common.Address)
	depositor := args[2].(common.Address)

	first, last := s.startingProposalID(), s.lastProposalID()
	var total uint64
	if last >= first {
		total = last - first + 1
	}
	start, end, nextKey, err := page(total, args[3].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]ProposalData, 0, end-start)
	for id := first + start; id < first+end; id++ {
		prop, ok := s.proposal(id)
		if !ok || (status != 0 && prop.status != status) {
			continue
		}
		if voter != (common.Address{}) && !s.votersByProposal(id).Contains(solstorage.AddressKey(voter)) {
			continue
		}
		if depositor != (common.Address{}) && !s.depositors(id).Contains(solstorage.AddressKey(depositor)) {
			continue
		}
		out = append(out, proposalData(s, id, prop))
	}
	return method.Outputs.Pack(out, nextKey)
}

func (p Precompile) voteQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	voter := args[1].(common.Address)
	options := s.vote(id, voter)
	if len(options) == 0 {
		return nil, fmt.Errorf("%w: proposal %d voter %s", errVoteNotFound, id, voter.Hex())
	}
	return method.Outputs.Pack(voteData(id, voter, options))
}

func (p Precompile) votesQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	voters := s.votersByProposal(id)
	start, end, nextKey, err := page(voters.Len(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]VoteData, 0, end-start)
	for i := start; i < end; i++ {
		voter := solstorage.MemberAddress(voters.At(i))
		out = append(out, voteData(id, voter, s.vote(id, voter)))
	}
	return method.Outputs.Pack(out, nextKey)
}

func (p Precompile) paramsQuery(s store, method *abi.Method) ([]byte, error) {
	params := s.params()
	return method.Outputs.Pack(GovParams{
		VotingPeriod:          params.VotingPeriod,
		ExpeditedVotingPeriod: params.ExpeditedVotingPeriod,
		MinDeposit:            params.MinDeposit,
		MaxDepositPeriod:      params.MaxDepositPeriod,
		MinExpeditedDeposit:   params.MinExpeditedDeposit,
		Quorum:                new(big.Int).SetUint64(params.Quorum),
		Threshold:             new(big.Int).SetUint64(params.Threshold),
		VetoThreshold:         new(big.Int).SetUint64(params.VetoThreshold),
		ExpeditedQuorum:       new(big.Int).SetUint64(params.ExpeditedQuorum),
		ExpeditedThreshold:    new(big.Int).SetUint64(params.ExpeditedThreshold),
	})
}

func (p Precompile) depositQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	depositor := args[1].(common.Address)
	amount := s.deposit(id, depositor)
	if amount.Sign() == 0 {
		return nil, fmt.Errorf("%w: proposal %d depositor %s", errDepositNotFound, id, depositor.Hex())
	}
	return method.Outputs.Pack(DepositData{ProposalId: id, Depositor: depositor, Amount: amount})
}

func (p Precompile) depositsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	depositors := s.depositors(id)
	start, end, nextKey, err := page(depositors.Len(), args[1].([]byte))
	if err != nil {
		return nil, err
	}
	out := make([]DepositData, 0, end-start)
	for i := start; i < end; i++ {
		depositor := solstorage.MemberAddress(depositors.At(i))
		out = append(out, DepositData{ProposalId: id, Depositor: depositor, Amount: s.deposit(id, depositor)})
	}
	return method.Outputs.Pack(out, nextKey)
}

func (p Precompile) tallyResultQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	prop, ok := s.proposal(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", errUnknownProposal, id)
	}
	if prop.status == StatusVotingPeriod {
		prop.recordTally(liveTally(s.State, p.Electorate, s, id))
	}
	return method.Outputs.Pack(tallyResult(prop))
}

func proposalData(s store, id uint64, prop proposal) ProposalData {
	contentType, content := s.proposalContent(id)
	return ProposalData{
		Id:               id,
		Status:           prop.status,
		FinalTallyResult: tallyResult(prop),
		SubmitTime:       prop.submitTime,
		DepositEndTime:   prop.depositEndTime,
		TotalDeposit:     prop.totalDeposit,
		VotingStartTime:  prop.votingStartTime,
		VotingEndTime:    prop.votingEndTime,
		IsExpedited:      prop.isExpedited,
		ContentType:      contentType,
		Content:          content,
	}
}

func tallyResult(prop proposal) TallyResultData {
	return TallyResultData{Yes: prop.tallyYes, Abstain: prop.tallyAbstain, No: prop.tallyNo, NoWithVeto: prop.tallyNoWithVeto}
}

func voteData(id uint64, voter common.Address, options []weightedVote) VoteData {
	out := make([]WeightedVoteOption, len(options))
	for i, o := range options {
		out[i] = WeightedVoteOption{Option: o.option, Weight: new(big.Int).SetUint64(o.weight)}
	}
	return VoteData{ProposalId: id, Voter: voter, Options: out}
}
