package accesscontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResourceTree(t *testing.T) {
	// validate that the tree is properly formed
	// each entry should have a parent that also has the resource defined as a child
	for resource, treeNode := range ResourceTree {
		// for the resource, get its parent
		// check for resource ANY (which has itself as a parent)
		if treeNode.Parent != resource {
			// verify that the parent has the child defined
			foundChild := false
			for _, child := range ResourceTree[treeNode.Parent].Children {
				if child == resource {
					foundChild = true
				}
			}
			require.True(t, foundChild)
		}
		// also check for each child having parent properly defined
		for _, child := range treeNode.Children {
			childParent := ResourceTree[child].Parent
			require.Equal(t, resource, childParent)
		}
	}
}
