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

// QueryServer is the server API for Query service.
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

var fileDescriptor_d41ee93a4668a185 = []byte{}