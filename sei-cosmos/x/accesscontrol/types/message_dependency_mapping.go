package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gogo/protobuf/proto"
)

func GenerateMessageKey(msg sdk.Msg) string {
	return proto.MessageName(msg)
}
