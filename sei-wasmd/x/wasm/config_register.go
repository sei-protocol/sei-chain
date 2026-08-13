package wasm

import (
	"strconv"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "wasm"

// wasmSchema declares the keys ReadWasmConfig resolves.
//
// A schema and not a transport: nothing decodes into it. The type the reader fills is
// types.WasmConfig, which carries no mapstructure tags, so no key can be derived from it. Declaring
// the spelling here is what lets the registry name the keys the reader looks up.
//
// The simulation limit is a string because that is the only shape the reader accepts for it. It takes
// the value only when the written one is a non-empty string and ignores every other shape, including a
// number, so declaring it as a number would put a value in the configuration that the reader drops.
// An empty string is how this says the limit is unset, which is what the reader does with it.
//
// types.WasmConfig also carries ContractDebugMode, which no key here reaches: the reader takes it from
// the node-wide trace setting. A field with no key of its own belongs to whichever section declares
// that setting, not to this one.
type wasmSchema struct {
	MemoryCacheSize    uint32 `mapstructure:"memory_cache_size"`
	QueryGasLimit      uint64 `mapstructure:"query_gas_limit"`
	SimulationGasLimit string `mapstructure:"simulation_gas_limit"`
}

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the schema, the defaults and the keys all come from
// one place. Nothing connects a schema field to the setting it stands for, so a test writes a value
// under each key and asks the reader which setting it reached.
func init() {
	registry.RegisterSection(SectionName, &wasmSchema{}, baseline)
}

// baseline is what this section resolves to for a node that has written nothing.
//
// Read out of the reader's own defaults rather than written again here, so a changed default moves
// both at once and this states only which key carries which setting. The same values for every mode:
// these bound work a contract may ask for, and every node runs the same contracts.
func baseline(registry.Mode) any {
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
