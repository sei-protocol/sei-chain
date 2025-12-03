package types

import (
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSeiAddressHandler_GetSeiAddressFromString(t *testing.T) {
	type args struct {
		address string
	}
	tests := []struct {
		name       string
		args       args
		want       types.AccAddress
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "returns address if address is valid",
			args: args{
				address: types.AccAddress("address").String(),
			},
			want: types.AccAddress("address"),
		},
		{
			name: "returns error if address is invalid",
			args: args{
				address: "invalid",
			},
			wantErr:    true,
			wantErrMsg: "decoding bech32 failed: invalid bech32 string length 7",
		}, {
			name: "returns error if address is empty",
			args: args{
				address: "",
			},
			wantErr:    true,
			wantErrMsg: "empty address string is not allowed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := SeiAddressHandler{}
			got, err := h.GetSeiAddressFromString(types.Context{}, tt.args.address)
			if tt.wantErr {
				require.NotNil(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
				return
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}
