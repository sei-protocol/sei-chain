package accesscontrol

import (
	abci "github.com/tendermint/tendermint/abci/types"
)

var (
	EmptyPrefix   = []byte{}
	ParentNodeKey = "ParentNode"
)

type StoreKeyToResourceTypePrefixMap map[string]map[ResourceType][]byte
type ResourceTypeToStoreKeyMap map[ResourceType]string

func DefaultStoreKeyToResourceTypePrefixMap() StoreKeyToResourceTypePrefixMap {
	return StoreKeyToResourceTypePrefixMap{
		ParentNodeKey: {
			ResourceType_ANY:     EmptyPrefix,
			ResourceType_KV:      EmptyPrefix,
			ResourceType_Mem:     EmptyPrefix,
			ResourceType_KV_WASM: EmptyPrefix,
		},
	}
}

type MsgValidator struct {
	storeKeyToResourceTypePrefixMap StoreKeyToResourceTypePrefixMap
}

func NewMsgValidator(storeKeyToResourceTypePrefixMap StoreKeyToResourceTypePrefixMap) *MsgValidator {
	return &MsgValidator{
		storeKeyToResourceTypePrefixMap: storeKeyToResourceTypePrefixMap,
	}
}

// GetPrefix tries to get the prefix for the ResourceType from the StoreKey Mapping
// and the default mapping, if it doesn't exist in either then it will return a nil, false
func (validator *MsgValidator) GetPrefix(storeKey string, resourceType ResourceType) ([]byte, bool) {
	if resourcePrefixMap, ok := validator.storeKeyToResourceTypePrefixMap[storeKey]; ok {
		if val, ok := resourcePrefixMap[resourceType]; ok {
			return val, true
		}
	}

	// Check if the resource type in one of the parent nodes where the identifier has to be *
	if resourcePrefixMap, ok := validator.storeKeyToResourceTypePrefixMap[ParentNodeKey]; ok {
		if val, ok := resourcePrefixMap[resourceType]; ok {
			return val, true
		}
	}

	return nil, false
}

// ValidateAccessOperations compares a list of events and a predefined list of access operations and determines if all the
// events that occurred are represented in the accessOperations
func (validator *MsgValidator) ValidateAccessOperations(accessOps []AccessOperation, events []abci.Event) map[Comparator]bool {
	eventsComparators := BuildComparatorFromEvents(events, validator.storeKeyToResourceTypePrefixMap)
	missingAccessOps := make(map[Comparator]bool)

	// If it's using default synchronous access op mapping then no need to verify
	if IsDefaultSynchronousAccessOps(accessOps) {
		return missingAccessOps
	}

	for _, eventComparator := range eventsComparators {
		if eventComparator.IsConcurrentSafeIdentifier() {
			continue
		}
		storeKey := eventComparator.StoreKey
		matched := false
		for _, accessOp := range accessOps {
			prefix, ok := validator.GetPrefix(storeKey, accessOp.GetResourceType())

			// The resource type was not a parent type where it could match anything nor was it found in the respective store key mapping
			if !ok {
				matched = false
				continue
			}

			if eventComparator.DependencyMatch(accessOp, prefix) {
				matched = true
				break
			}
		}

		if !matched {
			missingAccessOps[eventComparator] = true
		}
	}

	return missingAccessOps
}
