package types

import (
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/gogo/protobuf/proto"
)

var ErrNoCommitAccessOp = fmt.Errorf("MessageDependencyMapping doesn't terminate with AccessType_COMMIT")

func GenerateMessageKey(msg sdk.Msg) string {
	return proto.MessageName(msg)
}

func ValidateMessageDependencyMapping(mapping acltypes.MessageDependencyMapping) error {
	lastAccessOp := mapping.AccessOps[len(mapping.AccessOps)-1]
	if lastAccessOp.AccessType != acltypes.AccessType_COMMIT {
		return ErrNoCommitAccessOp
	}
	return nil
}
