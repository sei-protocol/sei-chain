package keys

import "fmt"

// Cosmos-SDK module store keys mounted on the memiavl backend in default
// production deployments. Defined as raw string literals (rather than
// re-exporting from x/* packages) to keep this package free of the heavy
// cosmos-sdk / ibc-go / wasmd / go-ethereum dependency closure.
//
// These string values are immutable on-disk format markers; changing any
// of them would break existing state.
const (
	AuthStoreKey         = "acc"          // sei-cosmos/x/auth/types.StoreKey
	AuthzStoreKey        = "authz"        // sei-cosmos/x/authz/keeper.StoreKey
	BankStoreKey         = "bank"         // sei-cosmos/x/bank/types.StoreKey
	StakingStoreKey      = "staking"      // sei-cosmos/x/staking/types.StoreKey
	MintStoreKey         = "mint"         // x/mint/types.StoreKey
	DistributionStoreKey = "distribution" // sei-cosmos/x/distribution/types.StoreKey
	SlashingStoreKey     = "slashing"     // sei-cosmos/x/slashing/types.StoreKey
	GovStoreKey          = "gov"          // sei-cosmos/x/gov/types.StoreKey
	ParamsStoreKey       = "params"       // sei-cosmos/x/params/types.StoreKey
	IBCStoreKey          = "ibc"          // sei-ibc-go/modules/core/24-host.StoreKey
	UpgradeStoreKey      = "upgrade"      // sei-cosmos/x/upgrade/types.StoreKey
	FeegrantStoreKey     = "feegrant"     // sei-cosmos/x/feegrant.StoreKey
	EvidenceStoreKey     = "evidence"     // sei-cosmos/x/evidence/types.StoreKey
	IBCTransferStoreKey  = "transfer"     // sei-ibc-go/modules/apps/transfer/types.StoreKey
	CapabilityStoreKey   = "capability"   // sei-cosmos/x/capability/types.StoreKey
	OracleStoreKey       = "oracle"       // x/oracle/types.StoreKey
	EVMStoreKey          = "evm"          // x/evm/types.StoreKey
	WasmStoreKey         = "wasm"         // sei-wasmd/x/wasm/types.StoreKey
	EpochStoreKey        = "epoch"        // x/epoch/types.StoreKey
	TokenfactoryStoreKey = "tokenfactory" // x/tokenfactory/types.StoreKey
)

// MemIAVLStoreKeys is the canonical list of module KV store keys that are
// mounted on the memiavl backend in a default production deployment.
// It must stay in lock-step with `app.kvStoreKeyNames` in app/app.go;
// TestKVStoreKeyNamesMatchMemIAVLStoreKeys (app/store_keys_test.go)
// enforces this.
var MemIAVLStoreKeys = []string{
	AuthStoreKey,
	AuthzStoreKey,
	BankStoreKey,
	StakingStoreKey,
	MintStoreKey,
	DistributionStoreKey,
	SlashingStoreKey,
	GovStoreKey,
	ParamsStoreKey,
	IBCStoreKey,
	UpgradeStoreKey,
	FeegrantStoreKey,
	EvidenceStoreKey,
	IBCTransferStoreKey,
	CapabilityStoreKey,
	OracleStoreKey,
	EVMStoreKey,
	WasmStoreKey,
	EpochStoreKey,
	TokenfactoryStoreKey,
}

// AllModulesExcept returns a list of modules excluding the specified modules.
// Returns an error if an excluded module is not a part of MemIAVLStoreKeys.
// The returned slice is safe to modify.
func AllModulesExcept(modulesNotToInclude ...string) ([]string, error) {
	exclude := make(map[string]bool, len(modulesNotToInclude))
	for _, m := range modulesNotToInclude {
		exclude[m] = true
	}

	known := make(map[string]bool, len(MemIAVLStoreKeys))
	for _, k := range MemIAVLStoreKeys {
		known[k] = true
	}
	for _, m := range modulesNotToInclude {
		if !known[m] {
			return nil, fmt.Errorf("module %q is not a member of MemIAVLStoreKeys", m)
		}
	}

	result := make([]string, 0, len(MemIAVLStoreKeys)-len(exclude))
	for _, k := range MemIAVLStoreKeys {
		if !exclude[k] {
			result = append(result, k)
		}
	}
	return result, nil
}
