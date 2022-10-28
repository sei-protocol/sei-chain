package accesscontrol

import "testing"

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
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "Unknown Access Type",
			fields: fields{AccessType: AccessType_READ, Identifier: "a/b/c/d", StoreKey: "123"},
			args: args{
				prefix: []byte("a"),
				accessOp: AccessOperation{
					AccessType: AccessType_UNKNOWN,
					IdentifierTemplate: "a/b/c",
					ResourceType: ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: true,
		},
		{
			name: "No contain",
			fields: fields{AccessType: AccessType_READ, Identifier: "a/b/c/d", StoreKey: "123"},
			args: args{
				prefix: []byte("a"),
				accessOp: AccessOperation{
					AccessType: AccessType_READ,
					IdentifierTemplate: "a/b/d/e/c",
					ResourceType: ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
		{
			name: "No Prefix comparator",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "c/b/c/d", StoreKey: "123"},
			args: args{
				prefix: []byte("a"),
				accessOp: AccessOperation{
					AccessType: AccessType_WRITE,
					IdentifierTemplate: "a/b/d/e/c",
					ResourceType: ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
		{
			name: "No Prefix accessop",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "a/b/c/d", StoreKey: "123"},
			args: args{
				prefix: []byte("a"),
				accessOp: AccessOperation{
					AccessType: AccessType_WRITE,
					IdentifierTemplate: "c/b/d/e/c",
					ResourceType: ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: false,
		},
		{
			name: "Star type",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "a/b/c/d", StoreKey: "123"},
			args: args{
				prefix: []byte("a"),
				accessOp: AccessOperation{
					AccessType: AccessType_WRITE,
					IdentifierTemplate: "*",
					ResourceType: ResourceType_KV_AUTH_ADDRESS_STORE,
				},
			},
			want: true,
		},
		{
			name: "Star type only when accesstype equal",
			fields: fields{AccessType: AccessType_READ, Identifier: "a/b/c/d", StoreKey: "123"},
			args: args{
				prefix: []byte("a"),
				accessOp: AccessOperation{
					AccessType: AccessType_WRITE,
					IdentifierTemplate: "*",
					ResourceType: ResourceType_KV_AUTH_ADDRESS_STORE,
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
				t.Errorf("Comparator.DependencyMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}
