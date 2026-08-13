package accesscontrol

import (
	"reflect"
	"testing"

	abci "github.com/tendermint/tendermint/abci/types"
)

func TestAccessTypeStringToEnum(t *testing.T) {
	type args struct {
		accessType string
	}
	tests := []struct {
		name string
		args args
		want AccessType
	}{
		{
			name: "read",
			args: args{accessType: "rEad"},
			want: AccessType_READ,
		},
		{
			name: "write",
			args: args{accessType: "wriTe"},
			want: AccessType_WRITE,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AccessTypeStringToEnum(tt.args.accessType); got != tt.want {
				t.Errorf("AccessTypeStringToEnum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComparator_IsConcurrentSafeIdentifier(t *testing.T) {
	type fields struct {
		AccessType AccessType
		Identifier string
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name:   "is safe",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "bank/SendEnabled"},
			want:   true,
		},
		{
			name:   "not safe",
			fields: fields{AccessType: AccessType_WRITE, Identifier: "some contract addr"},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Comparator{
				AccessType: tt.fields.AccessType,
				Identifier: tt.fields.Identifier,
			}
			if got := c.IsConcurrentSafeIdentifier(); got != tt.want {
				t.Errorf("Comparator.IsConcurrentSafeIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateAccessOperations(t *testing.T) {
	type args struct {
		accessOps []AccessOperation
		events    []abci.Event
	}
	tests := []struct {
		name string
		args args
		want map[Comparator]bool
	}{
		{
			name:   "empty",
			args:   args{
						accessOps: []AccessOperation{},
						events: []abci.Event{},
					},
			want:   map[Comparator]bool{},
		},
		{
			name:   "missing",
			args:   args{
						accessOps: []AccessOperation{},
						events: []abci.Event{
							{
								Type: "resource_access",
								Attributes: []abci.EventAttribute{
									{Key: []byte("key"), Value: []byte("a/b/c/d/e")},
									{Key: []byte("access_type"), Value: []byte("write")},
								},
							},
						},
					},
			want:   map[Comparator]bool{
				{AccessType: AccessType_WRITE, Identifier: "a/b/c/d/e"}: true,
			},
		},
		{
			name:   "missing with store key",
			args:   args{
						accessOps: []AccessOperation{},
						events: []abci.Event{
							{
								Type: "resource_access",
								Attributes: []abci.EventAttribute{
									{Key: []byte("key"), Value: []byte("a/b/c/d/e")},
									{Key: []byte("access_type"), Value: []byte("write")},
									{Key: []byte("store_key"), Value: []byte("storex")},
								},
							},
						},
					},
			want:   map[Comparator]bool{
				{AccessType: AccessType_WRITE, Identifier: "a/b/c/d/e", StoreKey: "storex"}: true,
			},
		},
		{
			name:   "extra access ops",
			args:   args{
						accessOps: []AccessOperation{
							{AccessType: AccessType_READ, IdentifierTemplate: "abc/defg", ResourceType: ResourceType_KV},
						},
						events: []abci.Event{},
					},
			want:   map[Comparator]bool{},
		},
		{
			name:   "matched",
			args:   args{
						accessOps: []AccessOperation{
							{AccessType: AccessType_WRITE, IdentifierTemplate: "abc/defg", ResourceType: ResourceType_KV},
						},
						events: []abci.Event{
							{
								Type: "resource_access",
								Attributes: []abci.EventAttribute{
									{Key: []byte("key"), Value: []byte("abc/defg/e")},
									{Key: []byte("access_type"), Value: []byte("write")},
								},
							},
						},
					},
			want:   map[Comparator]bool{},
		},
		{
			name:   "matched parent",
			args:   args{
						accessOps: []AccessOperation{
							{AccessType: AccessType_WRITE, IdentifierTemplate: "abc/defg", ResourceType: ResourceType_KV},
						},
						events: []abci.Event{
							{
								Type: "resource_access",
								Attributes: []abci.EventAttribute{
									{Key: []byte("key"), Value: []byte("abc/defg/e")},
									{Key: []byte("access_type"), Value: []byte("write")},
									{Key: []byte("store_key"), Value: []byte("ParentNode")},
								},
							},
						},
					},
			want:   map[Comparator]bool{},
		},
		{
			name:   "matched *",
			args:   args{
						accessOps: []AccessOperation{
							{AccessType: AccessType_WRITE, IdentifierTemplate: "*", ResourceType: ResourceType_KV},
						},
						events: []abci.Event{
							{
								Type: "resource_access",
								Attributes: []abci.EventAttribute{
									{Key: []byte("key"), Value: []byte("abc/defg/e")},
									{Key: []byte("access_type"), Value: []byte("write")},
									{Key: []byte("store_key"), Value: []byte("ParentNode")},
								},
							},
						},
					},
			want:   map[Comparator]bool{},
		},
		{
			name:   "matched UNKNOWN",
			args:   args{
						accessOps: []AccessOperation{
							{AccessType: AccessType_UNKNOWN, IdentifierTemplate: "abc/defg/e", ResourceType: ResourceType_KV},
						},
						events: []abci.Event{
							{
								Type: "resource_access",
								Attributes: []abci.EventAttribute{
									{Key: []byte("key"), Value: []byte("abc/defg/e")},
									{Key: []byte("access_type"), Value: []byte("write")},
									{Key: []byte("store_key"), Value: []byte("ParentNode")},
								},
							},
						},
					},
			want:   map[Comparator]bool{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewMsgValidator(DefaultStoreKeyToResourceTypePrefixMap())
			if got := validator.ValidateAccessOperations(tt.args.accessOps, tt.args.events); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ValidateAccessOperations() = %v, want %v", got, tt.want)
			}
		})
	}
}
