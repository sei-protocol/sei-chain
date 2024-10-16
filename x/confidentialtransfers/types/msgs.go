package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// confidential transfers message types
const (
	TypeMsgTransfer = "transfer"
)

var _ sdk.Msg = &MsgTransfer{}
