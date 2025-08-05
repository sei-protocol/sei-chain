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
	ErrDuplicateWasmMethodName           = fmt.Errorf("a method name is defined multiple times in specific access operation list")
	ErrQueryRefNonQueryMessageType       = fmt.Errorf("query contract references can only have query message types")
	ErrSelectorDeprecated                = fmt.Errorf("this selector type is deprecated")
	ErrInvalidMsgInfo                    = fmt.Errorf("msg info cannot be nil")
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

func SynchronousWasmAccessOps() []*acltypes.WasmAccessOperation {
	return []*acltypes.WasmAccessOperation{
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

func SynchronousAccessOpsSet() *AccessOperationSet {
	return NewAccessOperationSet(SynchronousAccessOps())
}

func SynchronousWasmDependencyMapping(contractAddress string) acltypes.WasmDependencyMapping {
	return acltypes.WasmDependencyMapping{
		BaseAccessOps:   SynchronousWasmAccessOps(),
		ContractAddress: contractAddress,
	}
}

func IsDefaultSynchronousWasmAccessOps(accessOps []*acltypes.WasmAccessOperation) bool {
	defaultAccessOps := SynchronousWasmAccessOps()
	for index, accessOp := range accessOps {
		if *accessOp != *defaultAccessOps[index] {
			return false
		}
	}
	return true
}

func IsCommitOp(accessOp *acltypes.AccessOperation) bool {
	return accessOp.AccessType == acltypes.AccessType_COMMIT
}

func DefaultMessageDependencyMapping() []acltypes.MessageDependencyMapping {
	return []acltypes.MessageDependencyMapping{}
}

func DefaultWasmDependencyMappings() []acltypes.WasmDependencyMapping {
	return []acltypes.WasmDependencyMapping{}
}

// Base access operation list must end with access type commit
func ValidateWasmDependencyMapping(mapping acltypes.WasmDependencyMapping) error {
	numOps := len(mapping.BaseAccessOps)
	if numOps == 0 || mapping.BaseAccessOps[numOps-1].Operation.AccessType != acltypes.AccessType_COMMIT {
		return ErrNoCommitAccessOp
	}

	// ensure uniqueness for partitioned message names across access ops and contract references
	seenMessageNames := map[string]struct{}{}
	for _, ops := range mapping.ExecuteAccessOps {
		if _, ok := seenMessageNames[ops.MessageName]; ok {
			return ErrDuplicateWasmMethodName
		}
		seenMessageNames[ops.MessageName] = struct{}{}
	}
	seenMessageNames = map[string]struct{}{}
	for _, ops := range mapping.QueryAccessOps {
		if _, ok := seenMessageNames[ops.MessageName]; ok {
			return ErrDuplicateWasmMethodName
		}
		seenMessageNames[ops.MessageName] = struct{}{}
	}
	seenMessageNames = map[string]struct{}{}
	for _, ops := range mapping.ExecuteContractReferences {
		if _, ok := seenMessageNames[ops.MessageName]; ok {
			return ErrDuplicateWasmMethodName
		}
		seenMessageNames[ops.MessageName] = struct{}{}
	}
	seenMessageNames = map[string]struct{}{}
	for _, ops := range mapping.QueryContractReferences {
		if _, ok := seenMessageNames[ops.MessageName]; ok {
			return ErrDuplicateWasmMethodName
		}
		seenMessageNames[ops.MessageName] = struct{}{}
	}

	// ensure deprecation for CONTRACT_REFERENCE access operation selector due to new contract references
	for _, accessOp := range mapping.BaseAccessOps {
		if accessOp.SelectorType == acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE {
			return ErrSelectorDeprecated
		}
	}
	for _, accessOps := range mapping.ExecuteAccessOps {
		for _, accessOp := range accessOps.WasmOperations {
			if accessOp.SelectorType == acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE {
				return ErrSelectorDeprecated
			}
		}
	}
	for _, accessOps := range mapping.QueryAccessOps {
		for _, accessOp := range accessOps.WasmOperations {
			if accessOp.SelectorType == acltypes.AccessOperationSelectorType_CONTRACT_REFERENCE {
				return ErrSelectorDeprecated
			}
		}
	}

	return nil
}
