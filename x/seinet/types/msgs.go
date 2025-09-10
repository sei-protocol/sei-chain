package types

import (
	"context"
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
)

// MsgCommitCovenant commits a covenant to the chain.
type MsgCommitCovenant struct {
	Creator  string         `json:"creator"`
	Covenant SeiNetCovenant `json:"covenant"`
}

// Route implements sdk.Msg.
func (m *MsgCommitCovenant) Route() string { return RouterKey }

// Type implements sdk.Msg.
func (m *MsgCommitCovenant) Type() string { return "CommitCovenant" }

// GetSigners returns the message signers.
func (m *MsgCommitCovenant) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes for message signing.
func (m *MsgCommitCovenant) GetSignBytes() []byte {
	bz, _ := json.Marshal(m)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic msg validation.
func (m *MsgCommitCovenant) ValidateBasic() error { return nil }

// MsgCommitCovenantResponse defines response.
type MsgCommitCovenantResponse struct{}

// MsgUnlockHardwareKey authorizes covenant commits for a signer.
type MsgUnlockHardwareKey struct {
	Creator string `json:"creator"`
}

// Route implements sdk.Msg.
func (m *MsgUnlockHardwareKey) Route() string { return RouterKey }

// Type implements sdk.Msg.
func (m *MsgUnlockHardwareKey) Type() string { return "UnlockHardwareKey" }

// GetSigners returns the message signers.
func (m *MsgUnlockHardwareKey) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes for message signing.
func (m *MsgUnlockHardwareKey) GetSignBytes() []byte {
	bz, _ := json.Marshal(m)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic msg validation.
func (m *MsgUnlockHardwareKey) ValidateBasic() error { return nil }

// MsgUnlockHardwareKeyResponse defines response.
type MsgUnlockHardwareKeyResponse struct{}

// MsgServer defines the gRPC msg server interface.
type MsgServer interface {
	CommitCovenant(context.Context, *MsgCommitCovenant) (*MsgCommitCovenantResponse, error)
	UnlockHardwareKey(context.Context, *MsgUnlockHardwareKey) (*MsgUnlockHardwareKeyResponse, error)
}

// RegisterMsgServer is a no-op placeholder to satisfy interface in Configurator.
func RegisterMsgServer(s grpc.ServiceRegistrar, srv MsgServer) {}

// QueryCovenantRequest queries final covenant.
type QueryCovenantRequest struct{}

// QueryCovenantResponse holds covenant string.
type QueryCovenantResponse struct {
	Covenant string `json:"covenant"`
}

// QueryServer defines gRPC query interface.
type QueryServer interface {
	Covenant(context.Context, *QueryCovenantRequest) (*QueryCovenantResponse, error)
}

// RegisterQueryServer is a no-op placeholder.
func RegisterQueryServer(s grpc.ServiceRegistrar, srv QueryServer) {}
