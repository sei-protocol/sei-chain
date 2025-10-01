// Code generated manually to support Seinet message proto definitions.
// source: seinet/msgs.proto

package types

import (
	context "context"
	"fmt"
	grpc1 "github.com/gogo/protobuf/grpc"
	proto "github.com/gogo/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
const _ = proto.GoGoProtoPackageIsVersion3
const _ = grpc.SupportPackageIsVersion4

// ----------------------
// 🔐 Message Definitions
// ----------------------

type MsgDepositToVault struct {
	Depositor string `protobuf:"bytes,1,opt,name=depositor,proto3" json:"depositor,omitempty"`
	Amount    string `protobuf:"bytes,2,opt,name=amount,proto3" json:"amount,omitempty"`
}

func (m *MsgDepositToVault) Reset()         { *m = MsgDepositToVault{} }
func (m *MsgDepositToVault) String() string { return proto.CompactTextString(m) }
func (*MsgDepositToVault) ProtoMessage()    {}
func (*MsgDepositToVault) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{0}
}

type MsgDepositToVaultResponse struct{}

func (m *MsgDepositToVaultResponse) Reset()         { *m = MsgDepositToVaultResponse{} }
func (m *MsgDepositToVaultResponse) String() string { return proto.CompactTextString(m) }
func (*MsgDepositToVaultResponse) ProtoMessage()    {}
func (*MsgDepositToVaultResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{1}
}

type MsgExecutePaywordSettlement struct {
	Executor     string `protobuf:"bytes,1,opt,name=executor,proto3" json:"executor,omitempty"`
	Recipient    string `protobuf:"bytes,2,opt,name=recipient,proto3" json:"recipient,omitempty"`
	Payword      string `protobuf:"bytes,3,opt,name=payword,proto3" json:"payword,omitempty"`
	CovenantHash string `protobuf:"bytes,4,opt,name=covenant_hash,json=covenantHash,proto3" json:"covenant_hash,omitempty"`
	Amount       string `protobuf:"bytes,5,opt,name=amount,proto3" json:"amount,omitempty"`
}

func (m *MsgExecutePaywordSettlement) Reset()         { *m = MsgExecutePaywordSettlement{} }
func (m *MsgExecutePaywordSettlement) String() string { return proto.CompactTextString(m) }
func (*MsgExecutePaywordSettlement) ProtoMessage()    {}
func (*MsgExecutePaywordSettlement) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{2}
}

type MsgExecutePaywordSettlementResponse struct{}

func (m *MsgExecutePaywordSettlementResponse) Reset()         { *m = MsgExecutePaywordSettlementResponse{} }
func (m *MsgExecutePaywordSettlementResponse) String() string { return proto.CompactTextString(m) }
func (*MsgExecutePaywordSettlementResponse) ProtoMessage()    {}
func (*MsgExecutePaywordSettlementResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{3}
}

var fileDescriptor_6e121d7b49b2de3c = []byte{}

// ----------------------
// 🔐 Client & Server API
// ----------------------

type MsgClient interface {
	DepositToVault(ctx context.Context, in *MsgDepositToVault, opts ...grpc.CallOption) (*MsgDepositToVaultResponse, error)
	ExecutePaywordSettlement(ctx context.Context, in *MsgExecutePaywordSettlement, opts ...grpc.CallOption) (*MsgExecutePaywordSettlementResponse, error)
}

type msgClient struct {
	cc grpc1.ClientConn
}

func NewMsgClient(cc grpc1.ClientConn) MsgClient {
	return &msgClient{cc}
}

func (c *msgClient) DepositToVault(ctx context.Context, in *MsgDepositToVault, opts ...grpc.CallOption) (*MsgDepositToVaultResponse, error) {
	out := new(MsgDepositToVaultResponse)
	err := c.cc.Invoke(ctx, "/seiprotocol.seichain.seinet.Msg/DepositToVault", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) ExecutePaywordSettlement(ctx context.Context, in *MsgExecutePaywordSettlement, opts ...grpc.CallOption) (*MsgExecutePaywordSettlementResponse, error) {
	out := new(MsgExecutePaywordSettlementResponse)
	err := c.cc.Invoke(ctx, "/seiprotocol.seichain.seinet.Msg/ExecutePaywordSettlement", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MsgServer is the server API for Msg service.
type MsgServer interface {
	DepositToVault(context.Context, *MsgDepositToVault) (*MsgDepositToVaultResponse, error)
	ExecutePaywordSettlement(context.Context, *MsgExecutePaywordSettlement) (*MsgExecutePaywordSettlementResponse, error)
}

// ----------------------
// ❌ UnimplementedMsgServer (for forward compatibility)
// ----------------------

type UnimplementedMsgServer struct{}

func (*UnimplementedMsgServer) DepositToVault(context.Context, *MsgDepositToVault) (*MsgDepositToVaultResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DepositToVault not implemented")
}
func (*UnimplementedMsgServer) ExecutePaywordSettlement(context.Context, *MsgExecutePaywordSettlement) (*MsgExecutePaywordSettlementResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ExecutePaywordSettlement not implemented")
}

func RegisterMsgServer(s grpc1.Server, srv MsgServer) {
	s.RegisterService(&_Msg_serviceDesc, srv)
}

func _Msg_DepositToVault_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgDepositToVault)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).DepositToVault(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/seiprotocol.seichain.seinet.Msg/DepositToVault",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).DepositToVault(ctx, req.(*MsgDepositToVault))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_ExecutePaywordSettlement_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgExecutePaywordSettlement)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).ExecutePaywordSettlement(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/seiprotocol.seichain.seinet.Msg/ExecutePaywordSettlement",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).ExecutePaywordSettlement(ctx, req.(*MsgExecutePaywordSettlement))
	}
	return interceptor(ctx, in, info, handler)
}

var _Msg_serviceDesc = grpc.ServiceDesc{
	ServiceName: "seiprotocol.seichain.seinet.Msg",
	HandlerType: (*MsgServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "DepositToVault",
			Handler:    _Msg_DepositToVault_Handler,
		},
		{
			MethodName: "ExecutePaywordSettlement",
			Handler:    _Msg_ExecutePaywordSettlement_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "seinet/msgs.proto",
}
