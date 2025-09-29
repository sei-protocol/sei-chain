package types

import (
	"context"

	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

type MsgServer interface {
	WithdrawWithSigil(context.Context, *MsgWithdrawWithSigil) (*MsgWithdrawWithSigilResponse, error)
}

type UnimplementedMsgServer struct{}

func (UnimplementedMsgServer) WithdrawWithSigil(context.Context, *MsgWithdrawWithSigil) (*MsgWithdrawWithSigilResponse, error) {
	return nil, ErrNotImplemented
}

type MsgWithdrawWithSigilResponse struct{}

func RegisterMsgServer(srv msgservice.Server, srvImpl MsgServer) {
	srv.RegisterService(&_Msg_serviceDesc, srvImpl)
}

var _Msg_serviceDesc = grpc.ServiceDesc{
	ServiceName: "kinvault.Msg",
	HandlerType: (*MsgServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "WithdrawWithSigil",
			Handler:    _Msg_WithdrawWithSigil_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "kinvault/tx.proto",
}

func _Msg_WithdrawWithSigil_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgWithdrawWithSigil)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).WithdrawWithSigil(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/kinvault.Msg/WithdrawWithSigil",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).WithdrawWithSigil(ctx, req.(*MsgWithdrawWithSigil))
	}
	return interceptor(ctx, in, info, handler)
}
