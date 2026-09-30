package gov

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// decPrecision is the number of decimal places a Dec carries, matching sdk.Dec.
const decPrecision = 18

// DecOne is the Dec value 1.
const DecOne Dec = 1_000_000_000_000_000_000

// MaxVoters bounds the voter set, and with it the work a tally does.
const MaxVoters = 1024

// MaxVotingPeriod bounds Params.VotingPeriod, in seconds.
const MaxVotingPeriod = math.MaxUint32

// Dec is a non-negative fixed-point fraction with 18 decimal places.
type Dec uint64

// ParseDec parses a decimal string such as "0.334".
func ParseDec(s string) (Dec, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || strings.ContainsAny(whole, "+-") || strings.ContainsAny(frac, "+-") || len(frac) > decPrecision {
		return 0, fmt.Errorf("invalid decimal %q", s)
	}
	w, err := strconv.ParseUint(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid decimal %q: %w", s, err)
	}
	var f uint64
	if frac != "" {
		f, err = strconv.ParseUint(frac+strings.Repeat("0", decPrecision-len(frac)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid decimal %q: %w", s, err)
		}
	}
	if w > (math.MaxUint64-f)/uint64(DecOne) {
		return 0, fmt.Errorf("decimal %q out of range", s)
	}
	return Dec(w*uint64(DecOne) + f), nil
}

// String formats d with 18 decimal places, as sdk.Dec does.
func (d Dec) String() string {
	return fmt.Sprintf("%d.%018d", uint64(d)/uint64(DecOne), uint64(d)%uint64(DecOne))
}

func (d Dec) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *Dec) UnmarshalText(text []byte) error {
	parsed, err := ParseDec(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d Dec) big() *big.Int { return new(big.Int).SetUint64(uint64(d)) }

// Params are the tally parameters of the governance precompile. Quorum and
// the thresholds are fractions of voting weight, as in Cosmos x/gov.
type Params struct {
	// VotingPeriod is how long a proposal stays open, in seconds of block time.
	VotingPeriod  uint64 `json:"voting_period"`
	Quorum        Dec    `json:"quorum"`
	Threshold     Dec    `json:"threshold"`
	VetoThreshold Dec    `json:"veto_threshold"`
}

// DefaultParams returns the Cosmos x/gov tally defaults with a one-hour voting period.
func DefaultParams() Params {
	return Params{
		VotingPeriod:  3600,
		Quorum:        334_000_000_000_000_000,
		Threshold:     500_000_000_000_000_000,
		VetoThreshold: 334_000_000_000_000_000,
	}
}

// Validate reports whether p is usable: a voting period in [1, MaxVotingPeriod]
// and every fraction in (0, 1].
func (p Params) Validate() error {
	if p.VotingPeriod == 0 || p.VotingPeriod > MaxVotingPeriod {
		return fmt.Errorf("voting period %d must be in [1, %d]", p.VotingPeriod, MaxVotingPeriod)
	}
	for name, v := range map[string]Dec{"quorum": p.Quorum, "threshold": p.Threshold, "veto threshold": p.VetoThreshold} {
		if v == 0 || v > DecOne {
			return fmt.Errorf("%s %s must be in (0, 1]", name, v)
		}
	}
	return nil
}

// Voter is an EVM address that votes with a validator's committee weight.
type Voter struct {
	Address common.Address
	Weight  uint64
}

// Genesis is the fixed voter set and tally parameters of the governance precompile.
type Genesis struct {
	Voters []Voter
	Params Params
}

// Validate reports whether g is usable: between 1 and MaxVoters voters with
// distinct non-zero addresses, positive weights that sum without overflow, and
// valid params.
func (g Genesis) Validate() error {
	if len(g.Voters) == 0 || len(g.Voters) > MaxVoters {
		return fmt.Errorf("voter count %d must be in [1, %d]", len(g.Voters), MaxVoters)
	}
	seen := make(map[common.Address]struct{}, len(g.Voters))
	var total uint64
	for _, voter := range g.Voters {
		if voter.Address == (common.Address{}) {
			return errors.New("voter address must not be zero")
		}
		if _, ok := seen[voter.Address]; ok {
			return fmt.Errorf("voter %s is listed twice", voter.Address)
		}
		seen[voter.Address] = struct{}{}
		if voter.Weight == 0 {
			return fmt.Errorf("voter %s has zero weight", voter.Address)
		}
		if voter.Weight > math.MaxUint64-total {
			return errors.New("total voter weight overflows uint64")
		}
		total += voter.Weight
	}
	return g.Params.Validate()
}

// sortedVoters returns the voters in ascending address order.
func sortedVoters(voters []Voter) []Voter {
	sorted := slices.Clone(voters)
	slices.SortFunc(sorted, func(a, b Voter) int { return bytes.Compare(a.Address[:], b.Address[:]) })
	return sorted
}
