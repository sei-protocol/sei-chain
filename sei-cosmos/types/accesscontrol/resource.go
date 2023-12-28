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
		ResourceType_KV_AUTH,
		ResourceType_KV_TOKENFACTORY,
		ResourceType_KV_DISTRIBUTION,
		ResourceType_KV_ACCESSCONTROL,
		ResourceType_KV_AUTHZ,
		ResourceType_KV_FEEGRANT,
		ResourceType_KV_SLASHING,
		ResourceType_KV_BANK_DEFERRED,
		ResourceType_KV_EVM,
	}},
	ResourceType_Mem: {ResourceType_ANY, []ResourceType{
		ResourceType_DexMem,
	}},
	ResourceType_DexMem: {ResourceType_Mem, []ResourceType{}},
	ResourceType_KV_BANK: {ResourceType_KV, []ResourceType{
		ResourceType_KV_BANK_SUPPLY,
		ResourceType_KV_BANK_DENOM,
		ResourceType_KV_BANK_BALANCES,
		ResourceType_KV_BANK_WEI_BALANCE,
	}},
	ResourceType_KV_BANK_SUPPLY:                   {ResourceType_KV_BANK, []ResourceType{}},
	ResourceType_KV_BANK_DENOM:                    {ResourceType_KV_BANK, []ResourceType{}},
	ResourceType_KV_BANK_BALANCES:                 {ResourceType_KV_BANK, []ResourceType{}},
	ResourceType_KV_BANK_DEFERRED:                 {ResourceType_KV, []ResourceType{ResourceType_KV_BANK_DEFERRED_MODULE_TX_INDEX}},
	ResourceType_KV_BANK_DEFERRED_MODULE_TX_INDEX: {ResourceType_KV_BANK_DEFERRED, []ResourceType{}},
	ResourceType_KV_BANK_WEI_BALANCE:              {ResourceType_KV_BANK, []ResourceType{}},
	ResourceType_KV_STAKING: {ResourceType_KV, []ResourceType{
		ResourceType_KV_STAKING_DELEGATION,
		ResourceType_KV_STAKING_VALIDATOR,
		ResourceType_KV_STAKING_VALIDATION_POWER,
		ResourceType_KV_STAKING_TOTAL_POWER,
		ResourceType_KV_STAKING_VALIDATORS_CON_ADDR,
		ResourceType_KV_STAKING_UNBONDING_DELEGATION,
		ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL,
		ResourceType_KV_STAKING_REDELEGATION,
		ResourceType_KV_STAKING_REDELEGATION_VAL_SRC,
		ResourceType_KV_STAKING_REDELEGATION_VAL_DST,
		ResourceType_KV_STAKING_REDELEGATION_QUEUE,
		ResourceType_KV_STAKING_VALIDATOR_QUEUE,
		ResourceType_KV_STAKING_HISTORICAL_INFO,
		ResourceType_KV_STAKING_UNBONDING,
		ResourceType_KV_STAKING_VALIDATORS_BY_POWER,
	}},
	ResourceType_KV_STAKING_DELEGATION:               {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_VALIDATOR:                {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_VALIDATORS_BY_POWER:      {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_VALIDATION_POWER:         {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_TOTAL_POWER:              {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_VALIDATORS_CON_ADDR:      {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_UNBONDING_DELEGATION:     {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_UNBONDING_DELEGATION_VAL: {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_REDELEGATION:             {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_REDELEGATION_VAL_SRC:     {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_REDELEGATION_VAL_DST:     {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_REDELEGATION_QUEUE:       {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_VALIDATOR_QUEUE:          {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_HISTORICAL_INFO:          {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_STAKING_UNBONDING:                {ResourceType_KV_STAKING, []ResourceType{}},
	ResourceType_KV_EPOCH:                            {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_ORACLE: {ResourceType_KV, []ResourceType{
		ResourceType_KV_ORACLE_AGGREGATE_VOTES,
		ResourceType_KV_ORACLE_VOTE_TARGETS,
		ResourceType_KV_ORACLE_FEEDERS,
		ResourceType_KV_ORACLE_EXCHANGE_RATE,
		ResourceType_KV_ORACLE_VOTE_PENALTY_COUNTER,
		ResourceType_KV_ORACLE_PRICE_SNAPSHOT,
	}},
	ResourceType_KV_ORACLE_VOTE_TARGETS:    {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_ORACLE_AGGREGATE_VOTES: {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_ORACLE_FEEDERS:         {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_DEX: {ResourceType_KV, []ResourceType{
		ResourceType_KV_DEX_CONTRACT_LONGBOOK,
		ResourceType_KV_DEX_CONTRACT_SHORTBOOK,
		ResourceType_KV_DEX_ORDER_BOOK,
		ResourceType_KV_DEX_PAIR_PREFIX,
		ResourceType_KV_DEX_TWAP,
		ResourceType_KV_DEX_PRICE,
		ResourceType_KV_DEX_CONTRACT,
		ResourceType_KV_DEX_SETTLEMENT_ENTRY,
		ResourceType_KV_DEX_REGISTERED_PAIR,
		ResourceType_KV_DEX_ORDER,
		ResourceType_KV_DEX_CANCEL,
		ResourceType_KV_DEX_ACCOUNT_ACTIVE_ORDERS,
		ResourceType_KV_DEX_ASSET_LIST,
		ResourceType_KV_DEX_NEXT_ORDER_ID,
		ResourceType_KV_DEX_NEXT_SETTLEMENT_ID,
		ResourceType_KV_DEX_MATCH_RESULT,
		ResourceType_KV_DEX_SETTLEMENT_ORDER_ID,
		ResourceType_KV_DEX_SETTLEMENT,
		ResourceType_KV_DEX_MEM_ORDER,
		ResourceType_KV_DEX_MEM_CANCEL,
		ResourceType_KV_DEX_MEM_DEPOSIT,
		ResourceType_KV_DEX_LONG_ORDER_COUNT,
		ResourceType_KV_DEX_SHORT_ORDER_COUNT,
		ResourceType_KV_DEX_MEM_CONTRACTS_TO_PROCESS,
		ResourceType_KV_DEX_MEM_DOWNSTREAM_CONTRACTS,
	}},
	ResourceType_KV_DEX_CONTRACT_LONGBOOK:     {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_CONTRACT_SHORTBOOK:    {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_ORDER_BOOK:            {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_CONTRACT:              {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_LONG_ORDER_COUNT:      {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_SHORT_ORDER_COUNT:     {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_PAIR_PREFIX:           {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_TWAP:                  {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_PRICE:                 {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_SETTLEMENT_ENTRY:      {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_REGISTERED_PAIR:       {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_ORDER:                 {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_CANCEL:                {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_ACCOUNT_ACTIVE_ORDERS: {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_ASSET_LIST:            {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_NEXT_ORDER_ID:         {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_NEXT_SETTLEMENT_ID:    {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_MATCH_RESULT:          {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_SETTLEMENT_ORDER_ID:   {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_SETTLEMENT:            {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_TOKENFACTORY: {ResourceType_KV, []ResourceType{
		ResourceType_KV_TOKENFACTORY_DENOM,
		ResourceType_KV_TOKENFACTORY_METADATA,
		ResourceType_KV_TOKENFACTORY_ADMIN,
		ResourceType_KV_TOKENFACTORY_CREATOR,
	}},
	ResourceType_KV_TOKENFACTORY_DENOM:    {ResourceType_KV_TOKENFACTORY, []ResourceType{}},
	ResourceType_KV_TOKENFACTORY_METADATA: {ResourceType_KV_TOKENFACTORY, []ResourceType{}},
	ResourceType_KV_TOKENFACTORY_ADMIN:    {ResourceType_KV_TOKENFACTORY, []ResourceType{}},
	ResourceType_KV_TOKENFACTORY_CREATOR:  {ResourceType_KV_TOKENFACTORY, []ResourceType{}},
	ResourceType_KV_AUTH: {ResourceType_KV, []ResourceType{
		ResourceType_KV_AUTH_ADDRESS_STORE,
		ResourceType_KV_AUTH_GLOBAL_ACCOUNT_NUMBER,
	}},
	ResourceType_KV_AUTH_ADDRESS_STORE:          {ResourceType_KV_AUTH, []ResourceType{}},
	ResourceType_KV_AUTH_GLOBAL_ACCOUNT_NUMBER:  {ResourceType_KV_AUTH, []ResourceType{}},
	ResourceType_KV_ORACLE_EXCHANGE_RATE:        {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_ORACLE_VOTE_PENALTY_COUNTER: {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_ORACLE_PRICE_SNAPSHOT:       {ResourceType_KV_ORACLE, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION: {ResourceType_KV, []ResourceType{
		ResourceType_KV_DISTRIBUTION_FEE_POOL,
		ResourceType_KV_DISTRIBUTION_PROPOSER_KEY,
		ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS,
		ResourceType_KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR,
		ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO,
		ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS,
		ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS,
		ResourceType_KV_DISTRIBUTION_VAL_ACCUM_COMMISSION,
		ResourceType_KV_DISTRIBUTION_SLASH_EVENT,
	}},
	ResourceType_KV_DISTRIBUTION_FEE_POOL:                {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_PROPOSER_KEY:            {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_OUTSTANDING_REWARDS:     {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR: {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_DELEGATOR_STARTING_INFO: {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS:  {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_VAL_CURRENT_REWARDS:     {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_VAL_ACCUM_COMMISSION:    {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_DISTRIBUTION_SLASH_EVENT:             {ResourceType_KV_DISTRIBUTION, []ResourceType{}},
	ResourceType_KV_ACCESSCONTROL: {ResourceType_KV, []ResourceType{
		ResourceType_KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING,
	}},
	ResourceType_KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING: {ResourceType_KV_ACCESSCONTROL, []ResourceType{}},
	ResourceType_KV_WASM: {ResourceType_KV, []ResourceType{
		ResourceType_KV_WASM_CODE,
		ResourceType_KV_WASM_CONTRACT_ADDRESS,
		ResourceType_KV_WASM_CONTRACT_STORE,
		ResourceType_KV_WASM_SEQUENCE_KEY,
		ResourceType_KV_WASM_CONTRACT_CODE_HISTORY,
		ResourceType_KV_WASM_CONTRACT_BY_CODE_ID,
		ResourceType_KV_WASM_PINNED_CODE_INDEX,
	}},
	ResourceType_KV_WASM_CODE:                         {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_WASM_CONTRACT_ADDRESS:             {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_WASM_CONTRACT_STORE:               {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_WASM_SEQUENCE_KEY:                 {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_WASM_CONTRACT_CODE_HISTORY:        {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_WASM_CONTRACT_BY_CODE_ID:          {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_WASM_PINNED_CODE_INDEX:            {ResourceType_KV_WASM, []ResourceType{}},
	ResourceType_KV_AUTHZ:                             {ResourceType_KV, []ResourceType{}},
	ResourceType_KV_FEEGRANT:                          {ResourceType_KV, []ResourceType{ResourceType_KV_FEEGRANT_ALLOWANCE}},
	ResourceType_KV_FEEGRANT_ALLOWANCE:                {ResourceType_KV_FEEGRANT, []ResourceType{}},
	ResourceType_KV_SLASHING:                          {ResourceType_KV, []ResourceType{ResourceType_KV_SLASHING_VAL_SIGNING_INFO, ResourceType_KV_SLASHING_ADDR_PUBKEY_RELATION_KEY}},
	ResourceType_KV_SLASHING_VAL_SIGNING_INFO:         {ResourceType_KV_SLASHING, []ResourceType{}},
	ResourceType_KV_SLASHING_ADDR_PUBKEY_RELATION_KEY: {ResourceType_KV_SLASHING, []ResourceType{}},
	ResourceType_KV_DEX_MEM_ORDER:                     {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_MEM_CANCEL:                    {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_MEM_DEPOSIT:                   {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_MEM_CONTRACTS_TO_PROCESS:      {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_DEX_MEM_DOWNSTREAM_CONTRACTS:      {ResourceType_KV_DEX, []ResourceType{}},
	ResourceType_KV_EVM: {ResourceType_KV, []ResourceType{
		ResourceType_KV_EVM_BALANCE,
		ResourceType_KV_EVM_TRANSIENT,
		ResourceType_KV_EVM_ACCOUNT_TRANSIENT,
		ResourceType_KV_EVM_MODULE_TRANSIENT,
		ResourceType_KV_EVM_NONCE,
		ResourceType_KV_EVM_RECEIPT,
		ResourceType_KV_EVM_S2E,
		ResourceType_KV_EVM_E2S,
		ResourceType_KV_EVM_CODE_HASH,
		ResourceType_KV_EVM_CODE,
		ResourceType_KV_EVM_CODE_SIZE,
	}},
	ResourceType_KV_EVM_BALANCE:           {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_TRANSIENT:         {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_ACCOUNT_TRANSIENT: {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_MODULE_TRANSIENT:  {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_NONCE:             {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_RECEIPT:           {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_S2E:               {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_E2S:               {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_CODE_HASH:         {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_CODE:              {ResourceType_KV_EVM, []ResourceType{}},
	ResourceType_KV_EVM_CODE_SIZE:         {ResourceType_KV_EVM, []ResourceType{}},
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
