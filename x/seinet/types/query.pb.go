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

var _ = proto.Marshal
var _ = fmt.Errorf
var _ context.Context
var _ grpc.ClientConn

const _ = proto.GoGoProtoPackageIsVersion3
const _ = grpc.SupportPackageIsVersion4

// -------------------------------
// 🔐 Request / Response Types
// -------------------------------

type QueryVaultBalanceRequest struct {
	Address string `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
}

func (m *QueryVaultBalanceRequest) Reset()         { *m = QueryVaultBalanceRequest{} }
func (m *QueryVaultBalanceRequest) String() string { return proto.CompactTextString(m) }
func (*QueryVaultBalanceRequest) ProtoMessage()    {}
func (*QueryVaultBalanceRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_498691af4bba20dd, []int{0}
}

type QueryVaultBalanceResponse struct {
	Balances []*QueryBalance `protobuf:"bytes,1,rep,name=balances,proto3" json:"balances,omitempty"`
}

func (m *QueryVaultBalanceResponse) Reset()         { *m = QueryVaultBalanceResponse{} }
func (m *QueryVaultBalanceResponse) String() string { return proto.CompactTextString(m) }
func (*QueryVaultBalanceResponse) ProtoMessage()    {}
func (*QueryVaultBalanceResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_498691af4bba20dd, []int{1}
}

type QueryCovenantBalanceRequest struct {
	Address string `protobuf:"bytes,1,opt,name=address,proto3" json:"address,omitempty"`
}

func (m *QueryCovenantBalanceRequest) Reset()         { *m = QueryCovenantBalanceRequest{} }
func (m *QueryCovenantBalanceRequest) String() string { return proto.CompactTextString(m) }
func (*QueryCovenantBalanceRequest) ProtoMessage()    {}
func (*QueryCovenantBalanceRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_498691af4bba20dd, []int{2}
}

type QueryCovenantBalanceResponse struct {
	Balances []*QueryBalance `protobuf:"bytes,1,rep,name=balances,proto3" json:"balances,omitempty"`
}

func (m *QueryCovenantBalanceResponse) Reset()         { *m = QueryCovenantBalanceResponse{} }
func (m *QueryCovenantBalanceResponse) String() string { return proto.CompactTextString(m) }
func (*QueryCovenantBalanceResponse) ProtoMessage()    {}
func (*QueryCovenantBalanceResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_498691af4bba20dd, []int{3}
}

type QueryBalance struct {
	Denom  string `protobuf:"bytes,1,opt,name=denom,proto3" json:"denom,omitempty"`
	Amount string `protobuf:"bytes,2,opt,name=amount,proto3" json:"amount,omitempty"`
}

func (m *QueryBalance) Reset()         { *m = QueryBalance{} }
func (m *QueryBalance) String() string { return proto.CompactTextString(m) }
func (*QueryBalance) ProtoMessage()    {}
func (*QueryBalance) Descriptor() ([]byte, []int) {
	return fileDescriptor_498691af4bba20dd, []int{4}
}

// -------------------------------
// 🔐 Query Client + Server Logic
// -------------------------------

type QueryClient interface {
	VaultBalance(ctx context.Context, in *QueryVaultBalanceRequest, opts ...grpc.CallOption) (*QueryVaultBalanceResponse, error)
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

type QueryServer interface {
	VaultBalance(context.Context, *QueryVaultBalanceRequest) (*QueryVaultBalanceResponse, error)
	CovenantBalance(context.Context, *QueryCovenantBalanceRequest) (*QueryCovenantBalanceResponse, error)
}

type UnimplementedQueryServer struct{}

func (*UnimplementedQueryServer) VaultBalance(context.Context, *QueryVaultBalanceRequest) (*QueryVaultBalanceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method VaultBalance not implemented")
}

func (*UnimplementedQueryServer) CovenantBalance(context.Context, *QueryCovenantBalanceRequest) (*QueryCovenantBalanceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CovenantBalance not implemented")
}

// -------------------------------
// 🔐 gRPC Handler Functions
// -------------------------------

func RegisterQueryServer(s grpc.ServiceRegistrar, srv QueryServer) {
	s.RegisterService(&_Query_serviceDesc, srv)
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

var fileDescriptor_498691af4bba20dd = []byte{}
