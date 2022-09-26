package types

import (
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gogo/protobuf/proto"
)

var ErrNoCommitAccessOp = fmt.Errorf("message dependency mapping AccessOps don't terminate with COMMIT access op")

func GenerateMessageKey(msg sdk.Msg) string {
	return proto.MessageName(msg)
}

func ValidateMessageDependencyMapping(mapping MessageDependencyMapping) error {
	lastAccessOp := mapping.AccessOps[len(mapping.AccessOps)-1]
	if lastAccessOp.AccessType != AccessType_COMMIT {
		return ErrNoCommitAccessOp
	}
	return nil
}
