package types

import (
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetAddressPrefix(t *testing.T) {
	type args struct {
		addr types.AccAddress
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "address is prefixed account prefix",
			args: args{
				addr: types.AccAddress([]byte{0x02, 0x03}),
			},
			want: []byte{0x01, 0x02, 0x02, 0x03},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, GetAddressPrefix(tt.args.addr), "GetAddressPrefix(%v)", tt.args.addr)
		})
	}
}

func TestGetAccountPrefixFromBech32(t *testing.T) {
	type args struct {
		addr string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "address is prefixed account prefix",
			args: args{
				addr: types.AccAddress([]byte{0x02, 0x03}).String(),
			},
			want: []byte{0x01, 0x02, 0x02, 0x03},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, GetAccountPrefixFromBech32(tt.args.addr), "GetAccountPrefixFromBech32(%v)", tt.args.addr)
		})
	}
}
