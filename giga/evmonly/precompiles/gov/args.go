package gov

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

var (
	errInvalidStatus  = errors.New("invalid proposal status")
	errTooManyChanges = errors.New("too many param changes")
)

// encodedChecks range-check a method's ABI-encoded arguments before they are
// decoded. A failure reverts the call with the error's message.
var encodedChecks = map[string]func(encoded []byte) error{
	SubmitParamChangeProposalMethod: checkChangeCount,
}

// maxParamChanges is the most changes a proposal may hold: one per declared param.
var maxParamChanges = big.NewInt(int64(len(chainparams.Keys())))

// checkChangeCount requires submitParamChangeProposal's encoded changes array
// to hold at most maxParamChanges entries. An encoding it cannot read is left
// for decoding to reject.
func checkChangeCount(encoded []byte) error {
	// Decoding copies an entry once per offset that points at it, so the count
	// is bounded before entries sharing one encoding are multiplied.
	const changesArg = 3
	offset, ok := encodedWord(encoded, changesArg*32)
	if !ok || !offset.IsUint64() {
		return nil
	}
	n, ok := encodedWord(encoded, offset.Uint64())
	if !ok || n.Cmp(maxParamChanges) <= 0 {
		return nil
	}
	return fmt.Errorf("%w: max %s", errTooManyChanges, maxParamChanges)
}

// encodedWord returns the word at byte at of encoded, reporting false when it
// is out of range.
func encodedWord(encoded []byte, at uint64) (*big.Int, bool) {
	if at > uint64(len(encoded)) || uint64(len(encoded))-at < 32 {
		return nil, false
	}
	return new(big.Int).SetBytes(encoded[at : at+32]), true
}

// argChecks range-check each method's arguments before it reads state. A
// failure reverts the call with the error's message.
var argChecks = map[string]func(args []interface{}) error{
	VoteMethod:               func(args []interface{}) error { return checkOption(args[1].(int32)) },
	VoteWeightedMethod:       func(args []interface{}) error { return checkWeightedOptions(weightedOptionsArg(args[1])) },
	SubmitTextProposalMethod: func(args []interface{}) error { return checkText(args[0].(string), args[1].(string)) },
	SubmitParamChangeProposalMethod: func(args []interface{}) error {
		if err := checkText(args[0].(string), args[1].(string)); err != nil {
			return err
		}
		return chainparams.CheckChanges(paramChangesArg(args[3]))
	},
	ProposalsMethod: checkProposalsArgs,
	VotesMethod:     func(args []interface{}) error { return checkPageKey(args[1].([]byte)) },
	DepositsMethod:  func(args []interface{}) error { return checkPageKey(args[1].([]byte)) },
}

func checkOption(option int32) error {
	if !validOption(option) {
		return fmt.Errorf("%w: %d", errInvalidVote, option)
	}
	return nil
}

func paramChangesArg(arg interface{}) []chainparams.Change {
	var changes []chainparams.Change
	abi.ConvertType(arg, &changes)
	return changes
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
