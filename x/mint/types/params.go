package types

import (
	"errors"
	"fmt"
	"strings"
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
		TokenReleaseSchedule: tokenReleaseSchedule,
	}
}

// default minting module parameters
func DefaultParams() Params {
	return Params{
		MintDenom: sdk.DefaultBondDenom,
		TokenReleaseSchedule: []ScheduledTokenRelease{
			{"2023-01-20", 123456789},
			{"2023-01-21", 123456789},
		},
	}
}

// validate params
func (p Params) Validate() error {
	if err := validateMintDenom(p.MintDenom); err != nil {
		return err
	}
	if err := validateTokenReleaseSchedule(p.TokenReleaseSchedule); err != nil {
		return err
	}
	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
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

func validateMintDenom(i interface{}) error {
	denomString, ok := i.(string)
	denomTrimed := strings.TrimSpace(denomString)

	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if denomTrimed == "" {
		return errors.New("mint denom cannot be blank")
	}
	if denomTrimed != sdk.DefaultBondDenom {
		return fmt.Errorf("mint denom must be the same as the default bond denom=%s", sdk.DefaultBondDenom)
	}
	if err := sdk.ValidateDenom(denomString); err != nil {
		return err
	}

	return nil
}

func validateTokenReleaseSchedule(i interface{}) error {
	tokenReleaseSchedule, ok := i.([]ScheduledTokenRelease)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	for _, scheduledTokenRelease := range tokenReleaseSchedule {
		if scheduledTokenRelease.GetTokenReleaseAmount() < 0 {
			return fmt.Errorf("token release amount must be non-negative: %d", scheduledTokenRelease.GetTokenReleaseAmount())
		}
		_, err := time.Parse(TokenReleaseDateFormat, scheduledTokenRelease.GetDate())
		if err != nil {
			return fmt.Errorf("error: invalid date format use yyyy-mm-dd: %s", err)
		}
	}
	return nil
}
