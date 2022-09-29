package accesscontrol

type TreeNode struct {
	Parent   ResourceType
	Children []ResourceType
}

// This returns a slice of all resource types that are dependent to a specific resource type
// Travel up the parents to add them all, and add all children as well.
// TODO: alternatively, hardcode all dependencies (parent and children) for a specific resource type, so we don't need to do a traversal when building dag
func (r ResourceType) GetResourceDependencies() []ResourceType {
	// resource is its own dependency
	resources := []ResourceType{r}
	resourceTree := map[ResourceType]TreeNode{
		ResourceType_ANY:    {ResourceType_ANY, []ResourceType{ResourceType_KV, ResourceType_Mem}},
		ResourceType_KV:     {ResourceType_ANY, []ResourceType{}},
		ResourceType_Mem:    {ResourceType_ANY, []ResourceType{ResourceType_DexMem}},
		ResourceType_DexMem: {ResourceType_Mem, []ResourceType{}},
	}
	// traverse up the parents chain
	currResource := r
	for currResource != ResourceType_ANY {
		parentResource := resourceTree[currResource].Parent
		// add parent resource
		resources = append(resources, parentResource)
		currResource = parentResource
	}

	// traverse children
	queue := resourceTree[r].Children
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		// add child to resource deps
		resources = append(resources, curr)
		// also need to traverse nested children
		queue = append(queue, resourceTree[curr].Children...)
	}

	return resources
}
