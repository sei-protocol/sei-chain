package gov

import (
	"errors"
	"fmt"
	"time"

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
	data, err := proposalData(s, id, prop)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(data)
}

// proposalsQuery pages through the narrowest index the filters allow: a
// voter's or depositor's proposals, a status queue, or every proposal id. A
// page covers a fixed span of that index, so a filtered page may hold fewer
// than pageLimit proposals while still returning a next key.
func (p Precompile) proposalsQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	status := args[0].(int32)
	voter := args[1].(common.Address)
	depositor := args[2].(common.Address)
	pageKey := args[3].([]byte)

	total, idAt := proposalIndex(s, status, voter, depositor)
	start, end, nextKey, err := page(total, pageKey)
	if err != nil {
		return nil, err
	}
	out := make([]ProposalData, 0, end-start)
	for i := start; i < end; i++ {
		id := idAt(i)
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
		data, err := proposalData(s, id, prop)
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return method.Outputs.Pack(out, nextKey)
}

// proposalIndex returns the length of the index proposalsQuery pages through
// and the proposal id at each position.
func proposalIndex(s store, status int32, voter, depositor common.Address) (uint64, func(uint64) uint64) {
	var set solstorage.Set
	switch {
	case voter != (common.Address{}):
		set = s.proposalsVotedBy(voter)
	case depositor != (common.Address{}):
		set = s.proposalsDepositedBy(depositor)
	case status == StatusDepositPeriod:
		return s.depositQueue().Len(), s.depositQueue().id
	case status == StatusVotingPeriod:
		return s.votingQueue().Len(), s.votingQueue().id
	default:
		first, last := s.startingProposalID(), s.lastProposalID()
		if last < first {
			return 0, nil
		}
		return last - first + 1, func(i uint64) uint64 { return first + i }
	}
	return set.Len(), func(i uint64) uint64 { return solstorage.MemberUint64(set.At(i)) }
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
		VotingPeriod:          durationSeconds(params.VotingPeriod),
		ExpeditedVotingPeriod: durationSeconds(params.ExpeditedVotingPeriod),
		MinDeposit:            coins(params.MinDeposit),
		MaxDepositPeriod:      durationSeconds(params.MaxDepositPeriod),
		MinExpeditedDeposit:   coins(params.MinExpeditedDeposit),
		Quorum:                formatDec(params.Quorum),
		Threshold:             formatDec(params.Threshold),
		VetoThreshold:         formatDec(params.VetoThreshold),
		ExpeditedQuorum:       formatDec(params.ExpeditedQuorum),
		ExpeditedThreshold:    formatDec(params.ExpeditedThreshold),
	})
}

func (p Precompile) depositQuery(s store, method *abi.Method, args []interface{}) ([]byte, error) {
	id := args[0].(uint64)
	depositor := args[1].(common.Address)
	amount := s.deposit(id, depositor)
	if amount.Sign() == 0 {
		return nil, fmt.Errorf("%w: proposal %d depositor %s", errDepositNotFound, id, depositor.Hex())
	}
	return method.Outputs.Pack(DepositData{ProposalId: id, Depositor: depositor.Hex(), Amount: coins(amount)})
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
		out = append(out, DepositData{ProposalId: id, Depositor: depositor.Hex(), Amount: coins(s.deposit(id, depositor))})
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

func proposalData(s store, id uint64, prop proposal) (ProposalData, error) {
	content, err := decodeContent(s.proposalContent(id))
	if err != nil {
		return ProposalData{}, err
	}
	return ProposalData{
		Id:               id,
		Status:           prop.status,
		FinalTallyResult: tallyResult(prop),
		SubmitTime:       unixSeconds(prop.submitTime),
		DepositEndTime:   unixSeconds(prop.depositEndTime),
		TotalDeposit:     coins(prop.totalDeposit),
		VotingStartTime:  unixSeconds(prop.votingStartTime),
		VotingEndTime:    unixSeconds(prop.votingEndTime),
		IsExpedited:      prop.isExpedited,
		Content:          content.v2JSON(),
	}, nil
}

// unixSeconds converts Unix nanoseconds to whole Unix seconds, rounding down
// as time.Time.Unix does.
func unixSeconds(nanos uint64) int64 {
	return int64(nanos / uint64(time.Second)) //nolint:gosec // below 2^35 after the division
}

// durationSeconds converts nanoseconds to whole seconds, rounding down as
// time.Duration.Seconds does when V2 casts it to uint64.
func durationSeconds(nanos uint64) uint64 {
	return nanos / uint64(time.Second)
}

func tallyResult(prop proposal) TallyResultData {
	return TallyResultData{
		Yes:        prop.tallyYes.String(),
		Abstain:    prop.tallyAbstain.String(),
		No:         prop.tallyNo.String(),
		NoWithVeto: prop.tallyNoWithVeto.String(),
	}
}

func voteData(id uint64, voter common.Address, options []weightedVote) VoteData {
	out := make([]WeightedVoteOptionData, len(options))
	for i, o := range options {
		out[i] = WeightedVoteOptionData{Option: o.option, Weight: formatDec(o.weight)}
	}
	return VoteData{ProposalId: id, Voter: voter.Hex(), Options: out}
}
