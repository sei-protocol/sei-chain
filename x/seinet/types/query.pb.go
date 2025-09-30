// Code generated manually to support QueryVaultBalance proto definitions.
// source: seinet/query.proto

package types

import (
	context "context"
	fmt "fmt"
	grpc1 "github.com/gogo/protobuf/grpc"
	proto "github.com/gogo/protobuf/proto"
	grpc "google.golang.org/grpc"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// QueryVaultBalanceRequest defines the input for querying the vault balance.
type QueryVaultBalanceRequest struct{}

func (m *QueryVaultBalanceRequest) Reset()         { *m = QueryVaultBalanceRequest{} }
func (m *QueryVaultBalanceRequest) String() string { return proto.CompactTextString(m) }
func (*QueryVaultBalanceRequest) ProtoMessage()    {}
func (*QueryVaultBalanceRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_2d6a97491b0c4fc8, []int{0}
}

func (m *QueryVaultBalanceRequest) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *QueryVaultBalanceRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_QueryVaultBalanceRequest.Marshal(b, m, deterministic)
	}
	return m.Marshal()
}

func (m *QueryVaultBalanceRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_QueryVaultBalanceRequest.Merge(m, src)
}

func (m *QueryVaultBalanceRequest) XXX_Size() int {
	return m.Size()
}

func (m *QueryVaultBalanceRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_QueryVaultBalanceRequest.DiscardUnknown(m)
}

var xxx_messageInfo_QueryVaultBalanceRequest proto.InternalMessageInfo

func (m *QueryVaultBalanceRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	_, err = m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:size], nil
}

func (m *QueryVaultBalanceRequest) MarshalTo([]byte) (int, error) { return 0, nil }

func (m *QueryVaultBalanceRequest) MarshalToSizedBuffer([]byte) (int, error) { return 0, nil }

func (m *QueryVaultBalanceRequest) Size() (n int) { return 0 }

func (m *QueryVaultBalanceRequest) Unmarshal(dAtA []byte) error {
	if len(dAtA) != 0 {
		return fmt.Errorf("proto: QueryVaultBalanceRequest: unexpected data length %d", len(dAtA))
	}
	return nil
}

// QueryVaultBalanceResponse defines the output containing the vault balance.
type QueryVaultBalanceResponse struct {
	Balance string `protobuf:"bytes,1,opt,name=balance,proto3" json:"balance,omitempty"`
}

func (m *QueryVaultBalanceResponse) Reset()         { *m = QueryVaultBalanceResponse{} }
func (m *QueryVaultBalanceResponse) String() string { return proto.CompactTextString(m) }
func (*QueryVaultBalanceResponse) ProtoMessage()    {}
func (*QueryVaultBalanceResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_2d6a97491b0c4fc8, []int{1}
}

func (m *QueryVaultBalanceResponse) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *QueryVaultBalanceResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_QueryVaultBalanceResponse.Marshal(b, m, deterministic)
	}
	return m.Marshal()
}

func (m *QueryVaultBalanceResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_QueryVaultBalanceResponse.Merge(m, src)
}

func (m *QueryVaultBalanceResponse) XXX_Size() int {
	return m.Size()
}

func (m *QueryVaultBalanceResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_QueryVaultBalanceResponse.DiscardUnknown(m)
}

var xxx_messageInfo_QueryVaultBalanceResponse proto.InternalMessageInfo

func (m *QueryVaultBalanceResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryVaultBalanceResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryVaultBalanceResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	if len(m.Balance) > 0 {
		i -= len(m.Balance)
		copy(dAtA[i:], m.Balance)
		i = encodeVarintQuery(dAtA, i, uint64(len(m.Balance)))
		i--
		dAtA[i] = 0x0a
	}
	return len(dAtA) - i, nil
}

func (m *QueryVaultBalanceResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	l := len(m.Balance)
	if l > 0 {
		n += 1 + l + sovQuery(uint64(l))
	}
	return n
}

func (m *QueryVaultBalanceResponse) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: wiretype end group for non-group")
			}
			if iNdEx >= l {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: unexpected EOF")
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: QueryVaultBalanceResponse: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: QueryVaultBalanceResponse: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Balance", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: QueryVaultBalanceResponse: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: QueryVaultBalanceResponse: unexpected EOF")
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: unexpected EOF")
			}
			m.Balance = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipQuery(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: invalid length")
			}
			if (iNdEx + skippy) > l {
				return fmt.Errorf("proto: QueryVaultBalanceResponse: unexpected EOF")
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return fmt.Errorf("proto: QueryVaultBalanceResponse: unexpected EOF")
	}
	return nil
}

func init() {
	proto.RegisterType((*QueryVaultBalanceRequest)(nil), "seiprotocol.seichain.seinet.QueryVaultBalanceRequest")
	proto.RegisterType((*QueryVaultBalanceResponse)(nil), "seiprotocol.seichain.seinet.QueryVaultBalanceResponse")
}

var fileDescriptor_2d6a97491b0c4fc8 = []byte{}

// QueryClient is the client API for Query service.
type QueryClient interface {
	VaultBalance(ctx context.Context, in *QueryVaultBalanceRequest, opts ...grpc.CallOption) (*QueryVaultBalanceResponse, error)
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

// QueryServer is the server API for Query service.
type QueryServer interface {
	VaultBalance(context.Context, *QueryVaultBalanceRequest) (*QueryVaultBalanceResponse, error)
}

// UnimplementedQueryServer can be embedded to have forward compatible implementations.
type UnimplementedQueryServer struct{}

func (*UnimplementedQueryServer) VaultBalance(context.Context, *QueryVaultBalanceRequest) (*QueryVaultBalanceResponse, error) {
	return nil, fmt.Errorf("method VaultBalance not implemented")
}

func RegisterQueryServer(s grpc1.Server, srv QueryServer) {
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

var _Query_serviceDesc = grpc.ServiceDesc{
	ServiceName: "seiprotocol.seichain.seinet.Query",
	HandlerType: (*QueryServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "VaultBalance",
			Handler:    _Query_VaultBalance_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "seinet/query.proto",
}

func sovQuery(x uint64) (n int) {
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}

func encodeVarintQuery(dAtA []byte, offset int, v uint64) int {
	offset -= sovQuery(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}

func skipQuery(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, fmt.Errorf("proto: wiretype end group for non-group")
			}
			if iNdEx >= l {
				return 0, fmt.Errorf("proto: unexpected EOF")
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for {
				if iNdEx >= l {
					return 0, fmt.Errorf("proto: unexpected EOF")
				}
				if dAtA[iNdEx] < 0x80 {
					iNdEx++
					break
				}
				iNdEx++
			}
		case 1:
			iNdEx += 8
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, fmt.Errorf("proto: unexpected EOF")
				}
				if iNdEx >= l {
					return 0, fmt.Errorf("proto: unexpected EOF")
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if length < 0 {
				return 0, fmt.Errorf("proto: invalid length")
			}
			iNdEx += length
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, fmt.Errorf("proto: invalid length")
		}
	}
	return iNdEx, nil
}
