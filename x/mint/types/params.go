package types

import (
	"fmt"
	"sort"
	"time"

	yaml "gopkg.in/yaml.v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// Parameter store keys
var (
	KeyMintDenom            = []byte("MintDenom")
	KeyTokenReleaseSchedule = []byte("TokenReleaseSchedule")
)

// ParamTable for minting module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func NewParams(
	mintDenom string, tokenReleaseSchedule []ScheduledTokenRelease,
) Params {
	return Params{
		MintDenom:            mintDenom,
		TokenReleaseSchedule: SortTokenReleaseCalendar(tokenReleaseSchedule),
	}
}

// default minting module parameters
func DefaultParams() Params {
	return Params{
		MintDenom:            sdk.DefaultBondDenom,
		TokenReleaseSchedule: []ScheduledTokenRelease{},
	}
}

// validate params
func (p Params) Validate() error {
	if err := validateMintDenom(p.MintDenom); err != nil {
		return err
	}
	return validateTokenReleaseSchedule(p.TokenReleaseSchedule)
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func (p Version2Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// Implements params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyMintDenom, &p.MintDenom, validateMintDenom),
		paramtypes.NewParamSetPair(KeyTokenReleaseSchedule, &p.TokenReleaseSchedule, validateTokenReleaseSchedule),
	}
}

// Used for v2 -> v3 migration
func (p *Version2Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyMintDenom, &p.MintDenom, func(i interface{}) error { return nil }),
		paramtypes.NewParamSetPair(KeyTokenReleaseSchedule, &p.TokenReleaseSchedule, func(i interface{}) error { return nil }),
	}
}

func validateMintDenom(i interface{}) error {
	denomString, ok := i.(string)

	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if denomString != sdk.DefaultBondDenom {
		return fmt.Errorf("mint denom must be the same as the default bond denom=%s", sdk.DefaultBondDenom)
	}

	return nil
}

func SortTokenReleaseCalendar(tokenReleaseSchedule []ScheduledTokenRelease) []ScheduledTokenRelease {
	sort.Slice(tokenReleaseSchedule, func(i, j int) bool {
		startDate1, _ := time.Parse(TokenReleaseDateFormat, tokenReleaseSchedule[i].GetStartDate())
		startDate2, _ := time.Parse(TokenReleaseDateFormat, tokenReleaseSchedule[j].GetStartDate())
		return startDate1.Before(startDate2)
	})
	return tokenReleaseSchedule
}

func validateTokenReleaseSchedule(i interface{}) error {
	tokenReleaseSchedule, ok := i.([]ScheduledTokenRelease)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	sortedTokenReleaseSchedule := SortTokenReleaseCalendar(tokenReleaseSchedule)

	prevReleaseEndDate := time.Time{}
	for _, scheduledTokenRelease := range sortedTokenReleaseSchedule {
		startDate, err := time.Parse(TokenReleaseDateFormat, scheduledTokenRelease.GetStartDate())
		if err != nil {
			return fmt.Errorf("error: invalid start date format use yyyy-mm-dd: %s", err)
		}

		endDate, err := time.Parse(TokenReleaseDateFormat, scheduledTokenRelease.GetEndDate())
		if err != nil {
			return fmt.Errorf("error: invalid end date format use yyyy-mm-dd: %s", err)
		}

		if startDate.After(endDate) {
			return fmt.Errorf("error: start date must be before end date %s > %s", startDate, endDate)
		}

		if startDate.Before(prevReleaseEndDate) {
			return fmt.Errorf("error: overlapping release period detected startDate=%s < prevReleaseEndDate=%s", startDate, prevReleaseEndDate)
		}
		prevReleaseEndDate = endDate
	}

	return nil
}
