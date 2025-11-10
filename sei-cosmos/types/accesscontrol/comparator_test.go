package accesscontrol

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComparator_DependencyMatch(t *testing.T) {
	type fields struct {
		AccessType AccessType
		Identifier string
		StoreKey   string
	}
	type args struct {
		accessOp AccessOperation
		prefix   []byte
	}
	prefixA, err := hex.DecodeString("0a")
	require.NoError(t, err)
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name:   "Unknown Access Type",
			fields: fields{AccessType: AccessType_READ, Identifier: "0abcdeff", StoreKey: "123"},
			args: args{
				prefix: prefixA,
				accessOp: AccessOperation{
					AccessType:         AccessType_UNKNOWN,
					IdentifierTemplate: "0abcde",
					ResourceType:       ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: true,
		},
		{
			name:   "No contain",
			fields: fields{AccessType: AccessType_READ, Identifier: "0abcde", StoreKey: "123"},
			args: args{
				prefix: prefixA,
				accessOp: AccessOperation{
					AccessType:         AccessType_READ,
					IdentifierTemplate: "0abdec",
					ResourceType:       ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
		{
			name:   "No Prefix comparator",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "0cbcde", StoreKey: "123"},
			args: args{
				prefix: prefixA,
				accessOp: AccessOperation{
					AccessType:         AccessType_WRITE,
					IdentifierTemplate: "0abdec",
					ResourceType:       ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
		{
			name:   "No Prefix accessop",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "0abcde", StoreKey: "123"},
			args: args{
				prefix: prefixA,
				accessOp: AccessOperation{
					AccessType:         AccessType_WRITE,
					IdentifierTemplate: "0cbdec",
					ResourceType:       ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
		{
			name:   "Star type",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "0abcde", StoreKey: "123"},
			args: args{
				prefix: prefixA,
				accessOp: AccessOperation{
					AccessType:         AccessType_WRITE,
					IdentifierTemplate: "*",
					ResourceType:       ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: true,
		},
		{
			name:   "Star type only when accesstype equal",
			fields: fields{AccessType: AccessType_READ, Identifier: "0abcde", StoreKey: "123"},
			args: args{
				prefix: prefixA,
				accessOp: AccessOperation{
					AccessType:         AccessType_WRITE,
					IdentifierTemplate: "*",
					ResourceType:       ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Comparator{
				AccessType: tt.fields.AccessType,
				Identifier: tt.fields.Identifier,
				StoreKey:   tt.fields.StoreKey,
			}
			if got := c.DependencyMatch(tt.args.accessOp, tt.args.prefix); got != tt.want {
				t.Errorf("Comparator.DependencyMatch() = '%v', want '%v' for comparator %v", got, tt.want, c)
			}
		})
	}
}
