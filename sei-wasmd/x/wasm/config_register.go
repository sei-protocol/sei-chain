package wasm

import (
	"strconv"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// SectionName is this section's name in the configuration key space.
const SectionName = "wasm"

// GeneratedQueryGasLimit is the smart-query gas limit a generated app.toml carries.
//
// The template interpolates this field rather than repeating the number, so the value an operator is handed
// and the value this section declares are one statement. They were two: the template wrote the number as a
// literal, so editing this constant moved the declaration and the rendered struct while a generated file
// kept the old value, and the test that pins the file against its own expected number stayed green. It
// fails now.
//
// A tenth of what this module's own default holds, and it is the value every node provisioned by the
// binary runs. Declared here, beside the section, and read by the command that renders the file, so the
// number that reaches an operator and the number this section states are one statement.
const GeneratedQueryGasLimit uint64 = 300_000

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
// Three keys, matching the three flag constants the module declares. Two settings of types.WasmConfig are
// deliberately absent, for different reasons. The contract debug switch is read from the node-wide trace
// flag, so its key belongs to the root of the file rather than to this section and this section cannot
// declare it; a consequence worth knowing is that these three keys do not determine the whole
// configuration the module ends up with. The cache size written as lru_size is put into app.toml by the
// template and read by nothing, so declaring it would offer a key that reaches no field.
func init() {
	registry.RegisterSection(SectionName, &wasmSchema{}, sectionDefaults)
}

// sectionDefaults is what the seid init command writes for a node of this kind.
//
// The query gas limit is a tenth of what this module's own default holds, and that is deliberate: it is
// the number the binary writes into every file it generates, so it is what every provisioned node runs.
// Declaring the module's larger default instead would have a caller rendering a file that loosens the
// only bound on the work one smart query can ask of a node serving queries to anyone. The module's
// default is still what a node whose file has no wasm section resolves, which is a different question and
// recorded as one.
//
// So this one key answers a different question from the two beside it, and a reader has to know which. The
// other two state what a node with nothing written runs, because they come from the module's own defaults.
// This one states what a generated file carries, and a node with no wasm section runs the module's larger
// limit instead.
//
// That matters to anything reading a resolution as a description of a running node. A report of one node's
// effective settings would name a bound ten times tighter than an un-sectioned node applies. It does not
// matter to anything reading a resolution as what to write, which is what the install does and what a
// renderer would do, and where writing the larger number is the outcome to avoid.
func sectionDefaults(registry.Mode) any {
	live := types.DefaultWasmConfig()
	schema := wasmSchema{
		MemoryCacheSize: live.MemoryCacheSize,
		QueryGasLimit:   GeneratedQueryGasLimit,
	}
	if live.SimulationGasLimit != nil {
		schema.SimulationGasLimit = strconv.FormatUint(*live.SimulationGasLimit, 10)
	}
	return schema
}
