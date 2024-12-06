package types

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/stretchr/testify/assert"
)

func TestDefaultParams(t *testing.T) {
	tests := []struct {
		name string
		want Params
	}{
		{
			name: "default params",
			want: Params{
				EnableCtModule:          DefaultEnableCtModule,
				RangeProofGasMultiplier: DefaultRangeProofGasMultiplier,
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
		EnableCtModule          bool
		RangeProofGasMultiplier uint32
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
				EnableCtModule:          true,
				RangeProofGasMultiplier: 10,
			},
			wantErr: false,
		},
		{
			name: "invalid params",
			fields: fields{
				EnableCtModule:          true,
				RangeProofGasMultiplier: 0,
			},
			wantErr: true,
			errMsg:  "range proof gas multiplier must be greater than 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Params{
				EnableCtModule:          tt.fields.EnableCtModule,
				RangeProofGasMultiplier: tt.fields.RangeProofGasMultiplier,
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

func TestValidateRangeProofGasMultiplier(t *testing.T) {
	t.Run("valid multiplier", func(t *testing.T) {
		multiplier := uint32(10)
		err := validateRangeProofGasMultiplier(multiplier)
		assert.Nil(t, err)
	})

	t.Run("valid but useless multiplier value", func(t *testing.T) {
		flag := uint32(1)
		err := validateRangeProofGasMultiplier(flag)
		assert.Nil(t, err)
	})

	t.Run("invalid multiplier value", func(t *testing.T) {
		flag := uint32(0)
		err := validateRangeProofGasMultiplier(flag)
		assert.Error(t, err)
	})

	t.Run("invalid multiplier type", func(t *testing.T) {
		flag := "True"
		err := validateRangeProofGasMultiplier(flag)
		assert.Error(t, err)
	})
}

func TestParams_ParamSetPairs(t *testing.T) {
	params := &Params{EnableCtModule: DefaultEnableCtModule, RangeProofGasMultiplier: DefaultRangeProofGasMultiplier}
	tests := []struct {
		name string
		want types.ParamSetPairs
	}{
		{
			name: "valid param set pairs",
			want: types.ParamSetPairs{
				types.NewParamSetPair(KeyEnableCtModule, &params.EnableCtModule, validateEnableCtModule),
				types.NewParamSetPair(KeyRangeProofGas, &params.RangeProofGasMultiplier, validateRangeProofGasMultiplier),
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
