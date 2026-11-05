package gov

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

const (
	decOne     = uint64(1_000_000_000_000_000_000)
	pageLimit  = 100 // x/gov's default
	pageKeyLen = 8
)

var (
	weiPerUsei = big.NewInt(1_000_000_000_000)

	errInvalidPageKey = errors.New("invalid page key")
)

// The ABI tuples. Field order matches each tuple's component order.
type (
	TallyResultData struct {
		Yes        *big.Int
		Abstain    *big.Int
		No         *big.Int
		NoWithVeto *big.Int
	}

	WeightedVoteOption struct {
		Option int32
		Weight *big.Int
	}

	ProposalData struct {
		Id               uint64
		Status           int32
		FinalTallyResult TallyResultData
		SubmitTime       uint64
		DepositEndTime   uint64
		TotalDeposit     *big.Int
		VotingStartTime  uint64
		VotingEndTime    uint64
		IsExpedited      bool
		ContentType      uint8
		Content          []byte
	}

	VoteData struct {
		ProposalId uint64
		Voter      common.Address
		Options    []WeightedVoteOption
	}

	DepositData struct {
		ProposalId uint64
		Depositor  common.Address
		Amount     *big.Int
	}

	GovParams struct {
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
)

// page returns the half-open range of a total-length index that pageKey
// selects, and the key of the page after it, empty on the last page.
func page(total uint64, pageKey []byte) (start, end uint64, nextKey []byte, err error) {
	if err := checkPageKey(pageKey); err != nil {
		return 0, 0, nil, err
	}
	if len(pageKey) != 0 {
		start = binary.BigEndian.Uint64(pageKey)
		if start > total {
			return 0, 0, nil, errInvalidPageKey
		}
	}
	end = min(start+pageLimit, total)
	if end < total {
		nextKey = binary.BigEndian.AppendUint64(nil, end)
	}
	return start, end, nextKey, nil
}
