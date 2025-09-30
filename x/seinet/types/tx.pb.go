// Code generated manually to support Seinet message proto definitions.
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

// MsgExecutePaywordSettlement defines a message for settling a revealed payword.
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

// MsgExecutePaywordSettlementResponse defines the gRPC response for a settlement request.
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

func init() {
	proto.RegisterType((*MsgDepositToVault)(nil), "seiprotocol.seichain.seinet.MsgDepositToVault")
	proto.RegisterType((*MsgDepositToVaultResponse)(nil), "seiprotocol.seichain.seinet.MsgDepositToVaultResponse")
	proto.RegisterType((*MsgExecutePaywordSettlement)(nil), "seiprotocol.seichain.seinet.MsgExecutePaywordSettlement")
	proto.RegisterType((*MsgExecutePaywordSettlementResponse)(nil), "seiprotocol.seichain.seinet.MsgExecutePaywordSettlementResponse")
}

var fileDescriptor_6e121d7b49b2de3c = []byte{}
