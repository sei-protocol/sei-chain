package types

import (
    "fmt"

    sdk "github.com/cosmos/cosmos-sdk/types"
    "google.golang.org/grpc"
)

// ----------------------------
// MsgCommitCovenant
// ----------------------------
func (m *MsgCommitCovenant) GetSigners() []sdk.AccAddress {
    addr, err := sdk.AccAddressFromBech32(m.Creator)
    if err != nil {
        panic(fmt.Sprintf("invalid creator address: %s", m.Creator))
    }
    return []sdk.AccAddress{addr}
}

func (m *MsgCommitCovenant) ValidateBasic() error {
    if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
        return fmt.Errorf("invalid creator address: %w", err)
    }
    // Add other covenant-specific validation here
    return nil
}

// ----------------------------
// MsgUnlockHardwareKey
// ----------------------------
func (m *MsgUnlockHardwareKey) GetSigners() []sdk.AccAddress {
    addr, err := sdk.AccAddressFromBech32(m.Creator)
    if err != nil {
        panic(fmt.Sprintf("invalid creator address: %s", m.Creator))
    }
    return []sdk.AccAddress{addr}
}

func (m *MsgUnlockHardwareKey) ValidateBasic() error {
    if _, err := sdk.AccAddressFromBech32(m.Creator); err != nil {
        return fmt.Errorf("invalid creator address: %w", err)
    }
    // Add other hardware key-specific validation here
    return nil
}

// ----------------------------
// Msg Server Registration
// ----------------------------
func RegisterMsgServer(s grpc.ServiceRegistrar, srv MsgServer) {
    // Use generated registration from protobuf
    RegisterSeinetMsgServer(s, srv)
}
