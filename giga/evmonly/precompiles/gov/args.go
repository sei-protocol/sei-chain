package gov

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

var errInvalidStatus = errors.New("invalid proposal status")

// argChecks range-check each method's arguments before it reads state. A
// failure reverts the call with the error's message.
var argChecks = map[string]func(args []interface{}) error{
	VoteMethod:               func(args []interface{}) error { return checkOption(args[1].(int32)) },
	VoteWeightedMethod:       func(args []interface{}) error { return checkWeightedOptions(weightedOptionsArg(args[1])) },
	SubmitTextProposalMethod: func(args []interface{}) error { return checkText(args[0].(string), args[1].(string)) },
	ProposalsMethod:          checkProposalsArgs,
	VotesMethod:              func(args []interface{}) error { return checkPageKey(args[1].([]byte)) },
	DepositsMethod:           func(args []interface{}) error { return checkPageKey(args[1].([]byte)) },
}

func checkOption(option int32) error {
	if !validOption(option) {
		return fmt.Errorf("%w: %d", errInvalidVote, option)
	}
	return nil
}

func weightedOptionsArg(arg interface{}) []WeightedVoteOption {
	var raw []WeightedVoteOption
	abi.ConvertType(arg, &raw)
	return raw
}

// checkWeightedOptions requires 1 to maxVoteOptions distinct valid options with
// nonzero weights summing to 1e18.
func checkWeightedOptions(raw []WeightedVoteOption) error {
	if len(raw) == 0 || len(raw) > maxVoteOptions {
		return fmt.Errorf("%w: between 1 and %d options required", errInvalidVote, maxVoteOptions)
	}
	seen := make(map[int32]bool, len(raw))
	total := new(big.Int)
	for _, o := range raw {
		if err := checkOption(o.Option); err != nil {
			return err
		}
		if seen[o.Option] {
			return fmt.Errorf("%w: %d repeated", errInvalidVote, o.Option)
		}
		seen[o.Option] = true
		if o.Weight.Sign() == 0 || o.Weight.Cmp(new(big.Int).SetUint64(decOne)) > 0 {
			return fmt.Errorf("%w: weight %s", errInvalidVote, o.Weight)
		}
		total.Add(total, o.Weight)
	}
	if !total.IsUint64() || total.Uint64() != decOne {
		return fmt.Errorf("%w: weights sum to %s, not 1e18", errInvalidVote, total)
	}
	return nil
}

// checkProposalsArgs requires a status of 0, for any, or a defined status, and
// a well-formed page key.
func checkProposalsArgs(args []interface{}) error {
	if status := args[0].(int32); status < 0 || status > StatusFailed {
		return fmt.Errorf("%w: %d", errInvalidStatus, status)
	}
	return checkPageKey(args[3].([]byte))
}

// checkPageKey requires an empty page key or one of pageKeyLen bytes.
func checkPageKey(pageKey []byte) error {
	if len(pageKey) != 0 && len(pageKey) != pageKeyLen {
		return errInvalidPageKey
	}
	return nil
}
