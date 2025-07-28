package scenarios

// ScenarioFactory is a function type that creates a new scenario instance
type ScenarioFactory func() TxGenerator

// scenarioFactories maps scenario names to their factory functions
var scenarioFactories = map[string]ScenarioFactory{
	// Manual entries for non-contract scenarios
	EVMTransfer: NewEVMTransferScenario,

	// Auto-generated entries will be added below this line by make generate
	// DO NOT EDIT BELOW THIS LINE - AUTO-GENERATED CONTENT
	ERC20Conflict: NewERC20ConflictScenario,
	ERC20Noop:     NewERC20NoopScenario,
	ERC20:         NewERC20Scenario,
	ERC721:        NewERC721Scenario,

	// DO NOT EDIT ABOVE THIS LINE - AUTO-GENERATED CONTENT
}

// CreateScenario creates a new scenario instance by name
func CreateScenario(name string) TxGenerator {
	factory, exists := scenarioFactories[name]
	if !exists {
		panic("Unknown scenario: " + name)
	}
	return factory()
}
