package utils_test

import (
	"fmt"
	"testing"

	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
)

func TestAllResourcesInTree(t *testing.T) {
	storeKeyToResourceMap := aclutils.StoreKeyToResourceTypePrefixMap
	resourceTree := sdkacltypes.ResourceTree

	storeKeyAllResourceTypes := make(map[sdkacltypes.ResourceType]bool)
	for _, resourceTypeToPrefix := range storeKeyToResourceMap {
		for resourceType := range resourceTypeToPrefix {
			storeKeyAllResourceTypes[resourceType] = true
		}
	}

	for resourceType := range resourceTree {
		if _, ok := storeKeyAllResourceTypes[resourceType]; !ok {
			panic(fmt.Sprintf("Missing resourceType=%s in the storekey to resource type prefix mapping", resourceType))
		}
	}

}
