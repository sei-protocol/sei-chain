package types

import (
	"fmt"
	"strings"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// DefaultEnableCtModule is the default value for the EnableCtModule flag.
const DefaultEnableCtModule = true

// DefaultRangeProofGasCost is the default value for RangeProofGasCost param.
const DefaultRangeProofGasCost = uint64(1000000)

// Enable usei and canonical usdc by default
const DefaultEnabledDenoms = "usei,uatom,uusdc,ibc/CA6FBFAF399474A06263E10D0CE5AEBBE15189D6D4B2DD9ADE61007E68EB9DB0"

// DefaultCiphertextGasCost is the default value for CiphertextGasCost param.
const DefaultCiphertextGasCost = uint64(10000)

const DefaultProofVerificationGasCost = uint64(20000)

// ParamKeyTable ParamTable for confidential transfers module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams default confidential transfers module parameters.
func DefaultParams() Params {
	enabledDenoms := strings.Split(DefaultEnabledDenoms, ",")
	return Params{
		EnableCtModule:           DefaultEnableCtModule,
		RangeProofGasCost:        DefaultRangeProofGasCost,
		EnabledDenoms:            enabledDenoms,
		CiphertextGasCost:        DefaultCiphertextGasCost,
		ProofVerificationGasCost: DefaultProofVerificationGasCost,
	}
}

// Validate validate params.
func (p *Params) Validate() error {
	if err := validateEnableCtModule(p.EnableCtModule); err != nil {
		return err
	}

	if err := validateRangeProofGasCost(p.RangeProofGasCost); err != nil {
		return err
	}

	if err := validateCiphertextGasCost(p.CiphertextGasCost); err != nil {
		return err
	}

	if err := validateProofVerificationGasCost(p.ProofVerificationGasCost); err != nil {
		return err
	}

	return nil
}

// ParamSetPairs Implements params.ParamSet.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyEnableCtModule, &p.EnableCtModule, validateEnableCtModule),
		paramtypes.NewParamSetPair(KeyRangeProofGas, &p.RangeProofGasCost, validateRangeProofGasCost),
		paramtypes.NewParamSetPair(KeyEnabledDenoms, &p.EnabledDenoms, validateEnabledDenoms),
		paramtypes.NewParamSetPair(KeyCiphertextGas, &p.CiphertextGasCost, validateCiphertextGasCost),
		paramtypes.NewParamSetPair(KeyProofVerificationGas, &p.ProofVerificationGasCost, validateProofVerificationGasCost),
	}
}

// Validator for the parameter.
func validateEnableCtModule(i interface{}) error {
	_, ok := i.(bool)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

// Validator for the parameter.
func validateRangeProofGasCost(i interface{}) error {
	_, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}

// Validator for the parameter.
func validateEnabledDenoms(i interface{}) error {
	_, ok := i.([]string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}

func validateCiphertextGasCost(i interface{}) error {
	_, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}

func validateProofVerificationGasCost(i interface{}) error {
	_, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}
