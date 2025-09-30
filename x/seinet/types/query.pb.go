// Code generated manually to support Query proto definitions.
// source: seiprotocol/seichain/seinet/query.proto

package types

import (
	context "context"
	fmt "fmt"

	grpc1 "github.com/gogo/protobuf/grpc"
	proto "github.com/gogo/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion3 // please upgrade the proto package

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// QueryVaultBalanceRequest represents the request payload for querying the vault balance.
type QueryVaultBalanceRequest struct {
	Address string `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
}

func (m *QueryVaultBalanceRequest) Reset()         { *m = QueryVaultBalanceRequest{} }
func (m *QueryVaultBalanceRequest) String() string { return proto.CompactTextString(m) }
func (*QueryVaultBalanceRequest) ProtoMessage()    {}
func (*QueryVaultBalanceRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_d41ee93a4668a185, []int{0}
}
func (m *QueryVaultBalanceRequest) XXX_Unmarshal(b []byte) error {
	return proto.Unmarshal(b, m)
}
func (m *QueryVaultBalanceRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return proto.Marshal(m)
}
func (m *QueryVaultBalanceRequest) XXX_Merge(src proto.Message) {
	proto.Merge(m, src)
}
func (m *QueryVaultBalanceRequest) XXX_Size() int {
	return proto.Size(m)
}
func (m *QueryVaultBalanceRequest) XXX_DiscardUnknown() {
	proto.DiscardUnknown(m)
}

// QueryVaultBalanceResponse represents the response payload for querying the vault balance.
type QueryVaultBalanceResponse struct {
	Balances []*QueryBalance `protobuf:"bytes,1,rep,name=balances,proto3" json:"balances,omitempty"`
}

func (m *QueryVaultBalanceResponse) Reset()         { *m = QueryVaultBalanceResponse{} }
func (m *QueryVaultBalanceResponse) String() string { return proto.CompactTextString(m) }
func (*QueryVaultBalanceResponse) ProtoMessage()    {}
func (*QueryVaultBalanceResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_d41ee93a4668a185, []int{1}
}
func (m *QueryVaultBalanceResponse) XXX_Unmarshal(b []byte) error {
	return proto.Unmarshal(b, m)
}
func (m *QueryVaultBalanceResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return proto.Marshal(m)
}
func (m *QueryVaultBalanceResponse) XXX_Merge(src proto.Message) {
	proto.Merge(m, src)
}
func (m *QueryVaultBalanceResponse) XXX_Size() int {
	return proto.Size(m)
}
func (m *QueryVaultBalanceResponse) XXX_DiscardUnknown() {
	proto.DiscardUnknown(m)
}

// QueryCovenantBalanceRequest represents the request payload for querying the covenant balance.
type QueryCovenantBalanceRequest struct {
	Address string `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
}

func (m *QueryCovenantBalanceRequest) Reset()         { *m = QueryCovenantBalanceRequest{} }
func (m *QueryCovenantBalanceRequest) String() string { return proto.CompactTextString(m) }
func (*QueryCovenantBalanceRequest) ProtoMessage()    {}
func (*QueryCovenantBalanceRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_d41ee93a4668a185, []int{2}
}
func (m *QueryCovenantBalanceRequest) XXX_Unmarshal(b []byte) error {
	return proto.Unmarshal(b, m)
}
func (m *QueryCovenantBalanceRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return proto.Marshal(m)
}
func (m *QueryCovenantBalanceRequest) XXX_Merge(src proto.Message) {
	proto.Merge(m, src)
}
func (m *QueryCovenantBalanceRequest) XXX_Size() int {
	return proto.Size(m)
}
func (m *QueryCovenantBalanceRequest) XXX_DiscardUnknown() {
	proto.DiscardUnknown(m)
}

// QueryCovenantBalanceResponse represents the response payload for querying the covenant balance.
type QueryCovenantBalanceResponse struct {
	Balances []*QueryBalance `protobuf:"bytes,1,rep,name=balances,proto3" json:"balances,omitempty"`
}

func (m *QueryCovenantBalanceResponse) Reset()         { *m = QueryCovenantBalanceResponse{} }
func (m *QueryCovenantBalanceResponse) String() string { return proto.CompactTextString(m) }
func (*QueryCovenantBalanceResponse) ProtoMessage()    {}
func (*QueryCovenantBalanceResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_d41ee93a4668a185, []int{3}
}
func (m *QueryCovenantBalanceResponse) XXX_Unmarshal(b []byte) error {
	return proto.Unmarshal(b, m)
}
func (m *QueryCovenantBalanceResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return proto.Marshal(m)
}
func (m *QueryCovenantBalanceResponse) XXX_Merge(src proto.Message) {
	proto.Merge(m, src)
}
func (m *QueryCovenantBalanceResponse) XXX_Size() int {
	return proto.Size(m)
}
func (m *QueryCovenantBalanceResponse) XXX_DiscardUnknown() {
	proto.DiscardUnknown(m)
}

// QueryBalance represents an individual balance returned from balance queries.
type QueryBalance struct {
	Denom  string `protobuf:"bytes,1,opt,name=denom,proto3" json:"denom,omitempty"`
	Amount string `protobuf:"bytes,2,opt,name=amount,proto3" json:"amount,omitempty"`
}

func (m *QueryBalance) Reset()         { *m = QueryBalance{} }
func (m *QueryBalance) String() string { return proto.CompactTextString(m) }
func (*QueryBalance) ProtoMessage()    {}
func (*QueryBalance) Descriptor() ([]byte, []int) {
	return fileDescriptor_d41ee93a4668a185, []int{4}
}
func (m *QueryBalance) XXX_Unmarshal(b []byte) error {
	return proto.Unmarshal(b, m)
}
func (m *QueryBalance) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return proto.Marshal(m)
}
func (m *QueryBalance) XXX_Merge(src proto.Message) {
	proto.Merge(m, src)
}
func (m *QueryBalance) XXX_Size() int {
	return proto.Size(m)
}
func (m *QueryBalance) XXX_DiscardUnknown() {
	proto.DiscardUnknown(m)
}

func init() {
	proto.RegisterType((*QueryVaultBalanceRequest)(nil), "seiprotocol.seichain.seinet.QueryVaultBalanceRequest")
	proto.RegisterType((*QueryVaultBalanceResponse)(nil), "seiprotocol.seichain.seinet.QueryVaultBalanceResponse")
	proto.RegisterType((*QueryCovenantBalanceRequest)(nil), "seiprotocol.seichain.seinet.QueryCovenantBalanceRequest")
	proto.RegisterType((*QueryCovenantBalanceResponse)(nil), "seiprotocol.seichain.seinet.QueryCovenantBalanceResponse")
	proto.RegisterType((*QueryBalance)(nil), "seiprotocol.seichain.seinet.QueryBalance")
}

var fileDescriptor_d41ee93a4668a185 = []byte{}

// QueryClient is the client API for Query service.
type QueryClient interface {
	// VaultBalance returns the balances held by the seinet vault module account.
	VaultBalance(ctx context.Context, in *QueryVaultBalanceRequest, opts ...grpc.CallOption) (*QueryVaultBalanceResponse, error)
	// CovenantBalance returns the balances held by the seinet covenant module account.
	CovenantBalance(ctx context.Context, in *QueryCovenantBalanceRequest, opts ...grpc.CallOption) (*QueryCovenantBalanceResponse, error)
}

type queryClient struct {
	cc grpc1.ClientConn
}

func NewQueryClient(cc grpc1.ClientConn) QueryClient {
	return &queryClient{cc}
}

func (c *queryClient) VaultBalance(ctx context.Context, in *QueryVaultBalanceRequest, opts ...grpc.CallOption) (*QueryVaultBalanceResponse, error) {
	out := new(QueryVaultBalanceResponse)
	err := c.cc.Invoke(ctx, "/seiprotocol.seichain.seinet.Query/VaultBalance", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *queryClient) CovenantBalance(ctx context.Context, in *QueryCovenantBalanceRequest, opts ...grpc.CallOption) (*QueryCovenantBalanceResponse, error) {
	out := new(QueryCovenantBalanceResponse)
	err := c.cc.Invoke(ctx, "/seiprotocol.seichain.seinet.Query/CovenantBalance", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// QueryServer is the server API for Query service.
type QueryServer interface {
	// VaultBalance returns the balances held by the seinet vault module account.
	VaultBalance(context.Context, *QueryVaultBalanceRequest) (*QueryVaultBalanceResponse, error)
	// CovenantBalance returns the balances held by the seinet covenant module account.
	CovenantBalance(context.Context, *QueryCovenantBalanceRequest) (*QueryCovenantBalanceResponse, error)
}

// UnimplementedQueryServer can be embedded to have forward compatible implementations.
type UnimplementedQueryServer struct{}

func (*UnimplementedQueryServer) VaultBalance(context.Context, *QueryVaultBalanceRequest) (*QueryVaultBalanceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VaultBalance not implemented")
}
func (*UnimplementedQueryServer) CovenantBalance(context.Context, *QueryCovenantBalanceRequest) (*QueryCovenantBalanceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CovenantBalance not implemented")
}

func RegisterQueryServer(s grpc1.Server, srv QueryServer) {
	s.RegisterService(&_Query_serviceDesc, srv)
}

var _Query_serviceDesc = grpc.ServiceDesc{
	ServiceName: "seiprotocol.seichain.seinet.Query",
	HandlerType: (*QueryServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "VaultBalance",
			Handler:    _Query_VaultBalance_Handler,
		},
		{
			MethodName: "CovenantBalance",
			Handler:    _Query_CovenantBalance_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "seiprotocol/seichain/seinet/query.proto",
}

func _Query_VaultBalance_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryVaultBalanceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).VaultBalance(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/seiprotocol.seichain.seinet.Query/VaultBalance",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).VaultBalance(ctx, req.(*QueryVaultBalanceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Query_CovenantBalance_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryCovenantBalanceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(QueryServer).CovenantBalance(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/seiprotocol.seichain.seinet.Query/CovenantBalance",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(QueryServer).CovenantBalance(ctx, req.(*QueryCovenantBalanceRequest))
	}
	return interceptor(ctx, in, info, handler)
}
