// Code generated manually to support MsgDepositToVault proto definitions.
// source: seinet/msgs.proto

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

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion3 // please upgrade the proto package

// MsgDepositToVault defines a message for depositing funds into the Seinet vault.
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

func (m *MsgDepositToVault) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *MsgDepositToVault) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgDepositToVault.Marshal(b, m, deterministic)
	}
	return m.Marshal()
}

func (m *MsgDepositToVault) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgDepositToVault.Merge(m, src)
}

func (m *MsgDepositToVault) XXX_Size() int {
	return m.Size()
}

func (m *MsgDepositToVault) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgDepositToVault.DiscardUnknown(m)
}

var xxx_messageInfo_MsgDepositToVault proto.InternalMessageInfo

func (m *MsgDepositToVault) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *MsgDepositToVault) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *MsgDepositToVault) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	if len(m.Amount) > 0 {
		i -= len(m.Amount)
		copy(dAtA[i:], m.Amount)
		i = encodeVarintMsgs(dAtA, i, uint64(len(m.Amount)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.Depositor) > 0 {
		i -= len(m.Depositor)
		copy(dAtA[i:], m.Depositor)
		i = encodeVarintMsgs(dAtA, i, uint64(len(m.Depositor)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *MsgDepositToVault) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Depositor)
	if l > 0 {
		n += 1 + l + sovMsgs(uint64(l))
	}
	l = len(m.Amount)
	if l > 0 {
		n += 1 + l + sovMsgs(uint64(l))
	}
	return n
}

func (m *MsgDepositToVault) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return fmt.Errorf("proto: MsgDepositToVault: wiretype end group for non-group")
			}
			if iNdEx >= l {
				return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
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
			return fmt.Errorf("proto: MsgDepositToVault: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: MsgDepositToVault: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Depositor", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: MsgDepositToVault: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
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
				return fmt.Errorf("proto: MsgDepositToVault: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: MsgDepositToVault: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
			}
			m.Depositor = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Amount", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: MsgDepositToVault: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
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
				return fmt.Errorf("proto: MsgDepositToVault: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: MsgDepositToVault: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
			}
			m.Amount = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipMsgs(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return fmt.Errorf("proto: MsgDepositToVault: invalid length")
			}
			if (iNdEx + skippy) > l {
				return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return fmt.Errorf("proto: MsgDepositToVault: unexpected EOF")
	}
	return nil
}

// MsgDepositToVaultResponse defines the gRPC response for a deposit request.
type MsgDepositToVaultResponse struct{}

func (m *MsgDepositToVaultResponse) Reset()         { *m = MsgDepositToVaultResponse{} }
func (m *MsgDepositToVaultResponse) String() string { return proto.CompactTextString(m) }
func (*MsgDepositToVaultResponse) ProtoMessage()    {}
func (*MsgDepositToVaultResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{1}
}

func (m *MsgDepositToVaultResponse) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *MsgDepositToVaultResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgDepositToVaultResponse.Marshal(b, m, deterministic)
	}
	return m.Marshal()
}

func (m *MsgDepositToVaultResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgDepositToVaultResponse.Merge(m, src)
}

func (m *MsgDepositToVaultResponse) XXX_Size() int {
	return m.Size()
}

func (m *MsgDepositToVaultResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgDepositToVaultResponse.DiscardUnknown(m)
}

var xxx_messageInfo_MsgDepositToVaultResponse proto.InternalMessageInfo

func (m *MsgDepositToVaultResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	_, err = m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:size], nil
}

func (m *MsgDepositToVaultResponse) MarshalTo([]byte) (int, error) {
	return 0, nil
}

func (m *MsgDepositToVaultResponse) MarshalToSizedBuffer([]byte) (int, error) {
	return 0, nil
}

func (m *MsgDepositToVaultResponse) Size() (n int) {
	return 0
}

func (m *MsgDepositToVaultResponse) Unmarshal(dAtA []byte) error {
	if len(dAtA) != 0 {
		return fmt.Errorf("proto: MsgDepositToVaultResponse: unexpected data length %d", len(dAtA))
	}
	return nil
}

// MsgExecutePaywordSettlement defines a message to settle covenant payouts.
type MsgExecutePaywordSettlement struct {
	Executor   string `protobuf:"bytes,1,opt,name=executor,proto3" json:"executor,omitempty"`
	CovenantId string `protobuf:"bytes,2,opt,name=covenant_id,json=covenantId,proto3" json:"covenant_id,omitempty"`
	Payee      string `protobuf:"bytes,3,opt,name=payee,proto3" json:"payee,omitempty"`
	Amount     string `protobuf:"bytes,4,opt,name=amount,proto3" json:"amount,omitempty"`
}

func (m *MsgExecutePaywordSettlement) Reset()         { *m = MsgExecutePaywordSettlement{} }
func (m *MsgExecutePaywordSettlement) String() string { return proto.CompactTextString(m) }
func (*MsgExecutePaywordSettlement) ProtoMessage()    {}
func (*MsgExecutePaywordSettlement) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{2}
}

func (m *MsgExecutePaywordSettlement) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *MsgExecutePaywordSettlement) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgExecutePaywordSettlement.Marshal(b, m, deterministic)
	}
	return m.Marshal()
}

func (m *MsgExecutePaywordSettlement) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgExecutePaywordSettlement.Merge(m, src)
}

func (m *MsgExecutePaywordSettlement) XXX_Size() int {
	return m.Size()
}

func (m *MsgExecutePaywordSettlement) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgExecutePaywordSettlement.DiscardUnknown(m)
}

var xxx_messageInfo_MsgExecutePaywordSettlement proto.InternalMessageInfo

func (m *MsgExecutePaywordSettlement) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *MsgExecutePaywordSettlement) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *MsgExecutePaywordSettlement) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	if len(m.Amount) > 0 {
		i -= len(m.Amount)
		copy(dAtA[i:], m.Amount)
		i = encodeVarintMsgs(dAtA, i, uint64(len(m.Amount)))
		i--
		dAtA[i] = 0x22
	}
	if len(m.Payee) > 0 {
		i -= len(m.Payee)
		copy(dAtA[i:], m.Payee)
		i = encodeVarintMsgs(dAtA, i, uint64(len(m.Payee)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.CovenantId) > 0 {
		i -= len(m.CovenantId)
		copy(dAtA[i:], m.CovenantId)
		i = encodeVarintMsgs(dAtA, i, uint64(len(m.CovenantId)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.Executor) > 0 {
		i -= len(m.Executor)
		copy(dAtA[i:], m.Executor)
		i = encodeVarintMsgs(dAtA, i, uint64(len(m.Executor)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *MsgExecutePaywordSettlement) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Executor)
	if l > 0 {
		n += 1 + l + sovMsgs(uint64(l))
	}
	l = len(m.CovenantId)
	if l > 0 {
		n += 1 + l + sovMsgs(uint64(l))
	}
	l = len(m.Payee)
	if l > 0 {
		n += 1 + l + sovMsgs(uint64(l))
	}
	l = len(m.Amount)
	if l > 0 {
		n += 1 + l + sovMsgs(uint64(l))
	}
	return n
}

func (m *MsgExecutePaywordSettlement) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: wiretype end group for non-group")
			}
			if iNdEx >= l {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
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
			return fmt.Errorf("proto: MsgExecutePaywordSettlement: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: MsgExecutePaywordSettlement: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Executor", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
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
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
			}
			m.Executor = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field CovenantId", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
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
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
			}
			m.CovenantId = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Payee", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
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
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
			}
			m.Payee = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Amount", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: wiretype end group for non-group")
				}
				if iNdEx >= l {
					return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
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
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			if postIndex > l {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
			}
			m.Amount = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipMsgs(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: invalid length")
			}
			if (iNdEx + skippy) > l {
				return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return fmt.Errorf("proto: MsgExecutePaywordSettlement: unexpected EOF")
	}
	return nil
}

// MsgExecutePaywordSettlementResponse defines the gRPC response for settlement execution.
type MsgExecutePaywordSettlementResponse struct{}

func (m *MsgExecutePaywordSettlementResponse) Reset()         { *m = MsgExecutePaywordSettlementResponse{} }
func (m *MsgExecutePaywordSettlementResponse) String() string { return proto.CompactTextString(m) }
func (*MsgExecutePaywordSettlementResponse) ProtoMessage()    {}
func (*MsgExecutePaywordSettlementResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_6e121d7b49b2de3c, []int{3}
}

func (m *MsgExecutePaywordSettlementResponse) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}

func (m *MsgExecutePaywordSettlementResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgExecutePaywordSettlementResponse.Marshal(b, m, deterministic)
	}
	return m.Marshal()
}

func (m *MsgExecutePaywordSettlementResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgExecutePaywordSettlementResponse.Merge(m, src)
}

func (m *MsgExecutePaywordSettlementResponse) XXX_Size() int {
	return m.Size()
}

func (m *MsgExecutePaywordSettlementResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgExecutePaywordSettlementResponse.DiscardUnknown(m)
}

var xxx_messageInfo_MsgExecutePaywordSettlementResponse proto.InternalMessageInfo

func (m *MsgExecutePaywordSettlementResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	_, err = m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:size], nil
}

func (m *MsgExecutePaywordSettlementResponse) MarshalTo([]byte) (int, error) {
	return 0, nil
}

func (m *MsgExecutePaywordSettlementResponse) MarshalToSizedBuffer([]byte) (int, error) {
	return 0, nil
}

func (m *MsgExecutePaywordSettlementResponse) Size() (n int) {
	return 0
}

func (m *MsgExecutePaywordSettlementResponse) Unmarshal(dAtA []byte) error {
	if len(dAtA) != 0 {
		return fmt.Errorf("proto: MsgExecutePaywordSettlementResponse: unexpected data length %d", len(dAtA))
	}
	return nil
}

func init() {
	proto.RegisterType((*MsgDepositToVault)(nil), "seiprotocol.seichain.seinet.MsgDepositToVault")
	proto.RegisterType((*MsgDepositToVaultResponse)(nil), "seiprotocol.seichain.seinet.MsgDepositToVaultResponse")
	proto.RegisterType((*MsgExecutePaywordSettlement)(nil), "seiprotocol.seichain.seinet.MsgExecutePaywordSettlement")
	proto.RegisterType((*MsgExecutePaywordSettlementResponse)(nil), "seiprotocol.seichain.seinet.MsgExecutePaywordSettlementResponse")
}

var fileDescriptor_6e121d7b49b2de3c = []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x34, 0xcc, 0xb1, 0x0a, 0x82, 0x40, 0x10, 0x85, 0xe1, 0x7a, 0xe2, 0x6e, 0x5d, 0xb8, 0x08, 0x82, 0xab, 0x88, 0xeb, 0xea, 0x15, 0x3c, 0x8b, 0xb8, 0x6b, 0x69, 0x6f, 0x42, 0x0b, 0x69, 0x33, 0xd0, 0x04, 0x0b, 0x1b, 0x9f, 0x7d, 0x10, 0x29, 0x9b, 0x71, 0x2f, 0xfe, 0xc7, 0x1f, 0x06, 0xfc, 0x2c, 0x18, 0xcc, 0xae, 0x5a, 0xd1, 0x6a, 0x1f, 0x0e, 0x6d, 0x0f, 0xb7, 0xad, 0x86, 0x14, 0xd8, 0x25, 0x8a, 0x67, 0x83, 0x84, 0x46, 0x0f, 0x8c, 0x31, 0xe6, 0xe4, 0xc6, 0x18, 0x86, 0x7d, 0x1f, 0x2c, 0x27, 0x1e, 0x9f, 0xee, 0x37, 0x63, 0xb8, 0x56, 0xa0, 0xd8, 0x7b, 0xec, 0xfa, 0x8e, 0x41, 0x19, 0xab, 0x98, 0xa5, 0xb2, 0x53, 0x46, 0x3f, 0x5a, 0x31, 0xbd, 0xf5, 0xee, 0xd6, 0xc0, 0xd7, 0xb6, 0xbd, 0xfc, 0x5d, 0x11, 0x00, 0x00, 0xff, 0xff, 0x8d, 0x1c, 0x03, 0xe4, 0xaf, 0x00, 0x00, 0x00}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// MsgClient is the client API for Msg service.
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

// UnimplementedMsgServer can be embedded to have forward compatible implementations.
type UnimplementedMsgServer struct{}

func (*UnimplementedMsgServer) DepositToVault(context.Context, *MsgDepositToVault) (*MsgDepositToVaultResponse, error) {
	return nil, fmt.Errorf("method DepositToVault not implemented")
}

func (*UnimplementedMsgServer) ExecutePaywordSettlement(context.Context, *MsgExecutePaywordSettlement) (*MsgExecutePaywordSettlementResponse, error) {
	return nil, fmt.Errorf("method ExecutePaywordSettlement not implemented")
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

func sovMsgs(x uint64) (n int) {
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}

func encodeVarintMsgs(dAtA []byte, offset int, v uint64) int {
	offset -= sovMsgs(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}

func skipMsgs(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	depth := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, fmt.Errorf("proto: too many bits")
			}
			if iNdEx >= l {
				return 0, fmt.Errorf("proto: unexpected EOF")
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
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
					return 0, fmt.Errorf("proto: length overflow")
				}
				if iNdEx >= l {
					return 0, fmt.Errorf("proto: unexpected EOF")
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if length < 0 {
				return 0, fmt.Errorf("proto: invalid length")
			}
			iNdEx += length
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, fmt.Errorf("proto: unexpected end of group")
			}
			depth--
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, fmt.Errorf("proto: invalid length")
		}
		if depth == 0 {
			return iNdEx, nil
		}
	}
	return 0, fmt.Errorf("proto: unexpected EOF")
}
