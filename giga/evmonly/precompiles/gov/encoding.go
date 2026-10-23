package gov

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const (
	decimals   = 18
	decOne     = uint64(1_000_000_000_000_000_000)
	baseDenom  = "usei"
	pageLimit  = 100 // x/gov's default, as V2 sets only the page key
	pageKeyLen = 8
)

var (
	weiPerUsei = big.NewInt(1_000_000_000_000)

	errInvalidPageKey = errors.New("invalid page key")
)

// The ABI tuples. Field order matches each tuple's component order.
type (
	Coin struct {
		Amount *big.Int
		Denom  string
	}

	TallyResultData struct {
		Yes        string
		Abstain    string
		No         string
		NoWithVeto string
	}

	WeightedVoteOptionData struct {
		Option int32
		Weight string
	}

	ProposalData struct {
		Id               uint64
		Status           int32
		FinalTallyResult TallyResultData
		SubmitTime       int64
		DepositEndTime   int64
		TotalDeposit     []Coin
		VotingStartTime  int64
		VotingEndTime    int64
		IsExpedited      bool
		Content          []byte
	}

	VoteData struct {
		ProposalId uint64
		Voter      string
		Options    []WeightedVoteOptionData
	}

	DepositData struct {
		ProposalId uint64
		Depositor  string
		Amount     []Coin
	}

	GovParams struct {
		VotingPeriod          uint64
		ExpeditedVotingPeriod uint64
		MinDeposit            []Coin
		MaxDepositPeriod      uint64
		MinExpeditedDeposit   []Coin
		Quorum                string
		Threshold             string
		VetoThreshold         string
		ExpeditedQuorum       string
		ExpeditedThreshold    string
	}
)

// coins renders a wei amount as usei coins, empty when the amount is zero.
func coins(wei *big.Int) []Coin {
	if wei.Sign() == 0 {
		return []Coin{}
	}
	return []Coin{{Amount: new(big.Int).Quo(wei, weiPerUsei), Denom: baseDenom}}
}

// parseDec parses a non-negative decimal string into its 1e18-scaled value.
func parseDec(s string) (uint64, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if (whole == "" && frac == "") || len(frac) > decimals || !digits(whole) || !digits(frac) {
		return 0, fmt.Errorf("invalid decimal %q", s)
	}
	frac += strings.Repeat("0", decimals-len(frac))
	v, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok || !v.IsUint64() {
		return 0, fmt.Errorf("decimal %q out of range", s)
	}
	return v.Uint64(), nil
}

func digits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// formatDec renders a 1e18-scaled value with 18 fractional digits.
func formatDec(v uint64) string {
	return strconv.FormatUint(v/decOne, 10) + "." + fmt.Sprintf("%018d", v%decOne)
}

// page returns the half-open range of a total-length index that pageKey
// selects, and the key of the page after it, empty on the last page.
func page(total uint64, pageKey []byte) (start, end uint64, nextKey []byte, err error) {
	if len(pageKey) != 0 {
		if len(pageKey) != pageKeyLen {
			return 0, 0, nil, errInvalidPageKey
		}
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
