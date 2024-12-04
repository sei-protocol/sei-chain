package types

import (
	"bytes"
	"github.com/cosmos/cosmos-sdk/x/params/types"
	"reflect"
	"testing"
)

func TestDefaultParams(t *testing.T) {
	tests := []struct {
		name string
		want Params
	}{
		{
			name: "default params",
			want: Params{
				DenomAllowlistMaxSize: 2000,
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
		DenomAllowlistMaxSize int32
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
				DenomAllowlistMaxSize: 2000,
			},
			wantErr: false,
		},
		{
			name: "invalid params",
			fields: fields{
				DenomAllowlistMaxSize: -1,
			},
			wantErr: true,
			errMsg:  "denom allowlist max size must be a non-negative integer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Params{
				DenomAllowlistMaxSize: tt.fields.DenomAllowlistMaxSize,
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

func Test_validateDenomAllowListMaxSize(t *testing.T) {
	type args struct {
		i interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid denom allowlist max size",
			args: args{
				i: int32(2000),
			},
			wantErr: false,
		},
		{
			name: "invalid denom allowlist max size",
			args: args{
				i: int32(-1),
			},
			wantErr: true,
			errMsg:  "denom allowlist max size must be a non-negative integer",
		},
		{
			name: "invalid denom allowlist large int value",
			args: args{
				i: 20000000000000,
			},
			wantErr: true,
			errMsg:  "invalid parameter type: int",
		},
		{
			name: "invalid denom allowlist max size type",
			args: args{
				i: "2000",
			},
			wantErr: true,
			errMsg:  "invalid parameter type: string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDenomAllowListMaxSize(tt.args.i)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDenomAllowListMaxSize() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && err.Error() != tt.errMsg {
				t.Errorf("validateDenomAllowListMaxSize() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestParams_ParamSetPairs(t *testing.T) {
	type fields struct {
		DenomAllowlistMaxSize int32
	}
	allowListSize := int32(20)
	params := &Params{DenomAllowlistMaxSize: allowListSize}
	tests := []struct {
		name   string
		fields fields
		want   types.ParamSetPairs
	}{
		{
			name: "valid param set pairs",
			fields: fields{
				DenomAllowlistMaxSize: allowListSize,
			},
			want: types.ParamSetPairs{
				types.NewParamSetPair([]byte("allowlistmaxsize"),
					&params.DenomAllowlistMaxSize,
					validateDenomAllowListMaxSize),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := params
			got := p.ParamSetPairs()
			if len(got) != len(tt.want) || !bytes.Equal(got[0].Key, tt.want[0].Key) || got[0].Value != tt.want[0].Value ||
				reflect.ValueOf(got[0].ValidatorFn).Pointer() != reflect.ValueOf(tt.want[0].ValidatorFn).Pointer() {
				t.Errorf("ParamSetPairs() = %v, want %v", got, tt.want)
			}
		})
	}
}
