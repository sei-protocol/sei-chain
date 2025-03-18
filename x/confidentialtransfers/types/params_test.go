package types

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/assert"
)

func TestDefaultParams(t *testing.T) {
	defaultEnabledDenoms := strings.Split(DefaultEnabledDenoms, ",")
	tests := []struct {
		name string
		want Params
	}{
		{
			name: "default params",
			want: Params{
				EnableCtModule:           DefaultEnableCtModule,
				RangeProofGasCost:        DefaultRangeProofGasCost,
				EnabledDenoms:            defaultEnabledDenoms,
				CiphertextGasCost:        DefaultCiphertextGasCost,
				ProofVerificationGasCost: DefaultProofVerificationGasCost,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultParams(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DefaultParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParams_Validate(t *testing.T) {
	type fields struct {
		EnableCtModule    bool
		RangeProofGasCost uint64
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid params",
			fields: fields{
				EnableCtModule:    true,
				RangeProofGasCost: 1000000,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Params{
				EnableCtModule:    tt.fields.EnableCtModule,
				RangeProofGasCost: tt.fields.RangeProofGasCost,
			}
			err := p.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && err.Error() != tt.errMsg {
				t.Errorf("Validate() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateEnableCtModule(t *testing.T) {
	t.Run("valid enable feature flag", func(t *testing.T) {
		flag := true
		err := validateEnableCtModule(flag)
		assert.Nil(t, err)
	})

	t.Run("invalid enable feature flag", func(t *testing.T) {
		flag := "True"
		err := validateEnableCtModule(flag)
		assert.Error(t, err)
	})
}

func TestValidateRangeProofGasCost(t *testing.T) {
	t.Run("valid cost", func(t *testing.T) {
		cost := uint64(1000000)
		err := validateRangeProofGasCost(cost)
		assert.Nil(t, err)
	})

	t.Run("valid but useless gas cost", func(t *testing.T) {
		flag := uint64(0)
		err := validateRangeProofGasCost(flag)
		assert.Nil(t, err)
	})

	t.Run("invalid gas cost", func(t *testing.T) {
		flag := -1
		err := validateRangeProofGasCost(flag)
		assert.Error(t, err)
	})

	t.Run("invalid gas cost type", func(t *testing.T) {
		flag := "True"
		err := validateRangeProofGasCost(flag)
		assert.Error(t, err)
	})
}

func TestParams_ParamSetPairs(t *testing.T) {
	defaultEnabledDenoms := strings.Split(DefaultEnabledDenoms, ",")

	params := &Params{
		EnableCtModule:           DefaultEnableCtModule,
		RangeProofGasCost:        DefaultRangeProofGasCost,
		EnabledDenoms:            defaultEnabledDenoms,
		CiphertextGasCost:        DefaultCiphertextGasCost,
		ProofVerificationGasCost: DefaultProofVerificationGasCost,
	}
	tests := []struct {
		name string
		want types.ParamSetPairs
	}{
		{
			name: "valid param set pairs",
			want: types.ParamSetPairs{
				types.NewParamSetPair(KeyEnableCtModule, &params.EnableCtModule, validateEnableCtModule),
				types.NewParamSetPair(KeyRangeProofGas, &params.RangeProofGasCost, validateRangeProofGasCost),
				types.NewParamSetPair(KeyEnabledDenoms, &params.EnabledDenoms, validateEnabledDenoms),
				types.NewParamSetPair(KeyCiphertextGas, &params.CiphertextGasCost, validateCiphertextGasCost),
				types.NewParamSetPair(KeyProofVerificationGas, &params.ProofVerificationGasCost, validateProofVerificationGasCost),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := params
			got := p.ParamSetPairs()
			if len(got) != len(tt.want) || !bytes.Equal(got[0].Key, tt.want[0].Key) ||
				got[0].Value != tt.want[0].Value ||
				reflect.ValueOf(got[0].ValidatorFn).Pointer() != reflect.ValueOf(tt.want[0].ValidatorFn).Pointer() {
				t.Errorf("ParamSetPairs() = %v, want %v", got, tt.want)
			}
		})
	}
}
