package types

import (
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/gogo/protobuf/proto"
)

var (
	ErrNoCommitAccessOp                  = fmt.Errorf("MessageDependencyMapping doesn't terminate with AccessType_COMMIT")
	ErrEmptyIdentifierString             = fmt.Errorf("IdentifierTemplate cannot be an empty string")
	ErrNonLeafResourceTypeWithIdentifier = fmt.Errorf("IdentifierTemplate must be '*' for non leaf resource types")
)

type MessageKey string

func GenerateMessageKey(msg sdk.Msg) MessageKey {
	return MessageKey(proto.MessageName(msg))
}

func CommitAccessOp() *acltypes.AccessOperation {
	return &acltypes.AccessOperation{ResourceType: acltypes.ResourceType_ANY, AccessType: acltypes.AccessType_COMMIT, IdentifierTemplate: "*"}
}

// Validates access operation sequence for a message, requires the last access operation to be a COMMIT
func ValidateAccessOps(accessOps []acltypes.AccessOperation) error {
	lastAccessOp := accessOps[len(accessOps)-1]
	if lastAccessOp != *CommitAccessOp() {
		return ErrNoCommitAccessOp
	}
	for _, accessOp := range accessOps {
		err := ValidateAccessOp(accessOp)
		if err != nil {
			return err
		}
	}

	return nil
}

func ValidateAccessOp(accessOp acltypes.AccessOperation) error {
	if accessOp.IdentifierTemplate == "" {
		return ErrEmptyIdentifierString
	}
	if accessOp.ResourceType.HasChildren() && accessOp.IdentifierTemplate != "*" {
		return ErrNonLeafResourceTypeWithIdentifier
	}
	return nil
}

func ValidateMessageDependencyMapping(mapping acltypes.MessageDependencyMapping) error {
	return ValidateAccessOps(mapping.AccessOps)
}

func SynchronousMessageDependencyMapping(messageKey MessageKey) acltypes.MessageDependencyMapping {
	return acltypes.MessageDependencyMapping{
		MessageKey:     string(messageKey),
		DynamicEnabled: true,
		AccessOps:      acltypes.SynchronousAccessOps(),
	}
}

func SynchronousAccessOps() []acltypes.AccessOperation {
	return []acltypes.AccessOperation{
		{AccessType: acltypes.AccessType_UNKNOWN, ResourceType: acltypes.ResourceType_ANY, IdentifierTemplate: "*"},
		*CommitAccessOp(),
	}
}

func SynchronousAccessOpsWithSelector() []acltypes.AccessOperationWithSelector {
	return []acltypes.AccessOperationWithSelector{
		{
			Operation:    &acltypes.AccessOperation{AccessType: acltypes.AccessType_UNKNOWN, ResourceType: acltypes.ResourceType_ANY, IdentifierTemplate: "*"},
			SelectorType: acltypes.AccessOperationSelectorType_NONE,
		},
		{
			Operation:    CommitAccessOp(),
			SelectorType: acltypes.AccessOperationSelectorType_NONE,
		},
	}
}

func IsDefaultSynchronousAccessOps(accessOps []acltypes.AccessOperation) bool {
	defaultAccessOps := SynchronousAccessOps()
	for index, accessOp := range accessOps {
		if accessOp != defaultAccessOps[index] {
			return false
		}
	}
	return true
}


func DefaultMessageDependencyMapping() []acltypes.MessageDependencyMapping {
	return []acltypes.MessageDependencyMapping{
		SynchronousMessageDependencyMapping(""),
	}
}

func DefaultWasmDependencyMappings() []acltypes.WasmDependencyMapping {
	return []acltypes.WasmDependencyMapping{}
}

func ValidateWasmDependencyMapping(mapping acltypes.WasmDependencyMapping) error {
	lastAccessOp := mapping.AccessOps[len(mapping.AccessOps)-1]
	if lastAccessOp.Operation.AccessType != acltypes.AccessType_COMMIT {
		return ErrNoCommitAccessOp
	}
	return nil
}
