package accesscontrol

type TreeNode struct {
	Parent   ResourceType
	Children []ResourceType
}

var ResourceTree = map[ResourceType]TreeNode{
	ResourceType_ANY: {ResourceType_ANY, []ResourceType{ResourceType_KV, ResourceType_Mem}},
	ResourceType_KV: {ResourceType_ANY, []ResourceType{
		ResourceType_KV_BANK,
		ResourceType_KV_DEX,
		ResourceType_KV_EPOCH,
		ResourceType_KV_ORACLE,
		ResourceType_KV_STAKING,
		ResourceType_KV_WASM,
		ResourceType_KV_TOKENFACTORY,
	}},
	ResourceType_Mem:                       {ResourceType_ANY, []ResourceType{ResourceType_DexMem}},
	ResourceType_DexMem:                    {ResourceType_Mem, []ResourceType{}},
	ResourceType_KV_BANK:                   {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_STAKING:                {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_WASM:                   {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_EPOCH:                  {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_ORACLE:                 {ResourceType_KV, []ResourceType{ResourceType_KV_ORACLE_AGGREGATE_VOTES, ResourceType_KV_ORACLE_VOTE_TARGETS, ResourceType_KV_ORACLE_FEEDERS}},
	ResourceType_KV_ORACLE_VOTE_TARGETS:    {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_ORACLE_AGGREGATE_VOTES: {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_ORACLE_FEEDERS:         {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_DEX:                    {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_TOKENFACTORY:           {ResourceType_KV, []ResourceType{}},
}

// This returns a slice of all resource types that are dependent to a specific resource type
// Travel up the parents to add them all, and add all children as well.
// TODO: alternatively, hardcode all dependencies (parent and children) for a specific resource type, so we don't need to do a traversal when building dag
func (r ResourceType) GetResourceDependencies() []ResourceType {
	// resource is its own dependency
	resources := []ResourceType{r}

	//get parents
	resources = append(resources, r.GetParentResources()...)

	// traverse children
	queue := ResourceTree[r].Children
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		// add child to resource deps
		resources = append(resources, curr)
		// also need to traverse nested children
		queue = append(queue, ResourceTree[curr].Children...)
	}

	return resources
}

func (r ResourceType) GetParentResources() []ResourceType {
	parentResources := []ResourceType{}

	// traverse up the parents chain
	currResource := r
	for currResource != ResourceType_ANY {
		parentResource := ResourceTree[currResource].Parent
		// add parent resource
		parentResources = append(parentResources, parentResource)
		currResource = parentResource
	}

	return parentResources
}

func (r ResourceType) HasChildren() bool {
	return len(ResourceTree[r].Children) > 0
}
