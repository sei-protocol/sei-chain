package wasm

import (
	"strconv"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "wasm"

// wasmSchema names the keys this module's reader resolves.
//
// A schema rather than types.WasmConfig itself, which carries no mapstructure tags at all, so registering
// it would derive keys from field names and those are not the keys the reader asks for. The schema states
// the three the module reads, and states them once.
//
// SimulationGasLimit is text because the field it stands for is an optional number, and absent is a
// meaning of its own: unset means the consensus block gas limit applies. A number cannot carry that, and
// the reader already parses this key from text.
type wasmSchema struct {
	MemoryCacheSize    uint32 `mapstructure:"memory_cache_size"`
	QueryGasLimit      uint64 `mapstructure:"query_gas_limit"`
	SimulationGasLimit string `mapstructure:"simulation_gas_limit"`
}

// Registration puts this section in the configuration registry.
//
// Three keys, matching the three flag constants above. Two settings of types.WasmConfig are deliberately
// absent: ContractDebugMode has no key any reader resolves, and lru_size is written into app.toml by the
// template and read by nothing, so declaring either would put a key in the space that reaches no field.
func init() {
	registry.RegisterSection(SectionName, &wasmSchema{}, defaults)
}

// defaults is what this section resolves to for a node that has written nothing.
func defaults(registry.Mode) any {
	live := types.DefaultWasmConfig()
	schema := wasmSchema{
		MemoryCacheSize: live.MemoryCacheSize,
		QueryGasLimit:   live.SmartQueryGasLimit,
	}
	if live.SimulationGasLimit != nil {
		schema.SimulationGasLimit = strconv.FormatUint(*live.SimulationGasLimit, 10)
	}
	return schema
}
