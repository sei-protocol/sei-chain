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
	ERC20: NewERC20Scenario,
	ERC721: NewERC721Scenario,

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

// GetAvailableScenarios returns a list of all available scenario names
func GetAvailableScenarios() []string {
	names := make([]string, 0, len(scenarioFactories))
	for name := range scenarioFactories {
		names = append(names, name)
	}
	return names
}

// RegisterScenario allows manual registration of scenario factories
// This is useful for adding custom scenarios that aren't auto-generated
func RegisterScenario(name string, factory ScenarioFactory) {
	scenarioFactories[name] = factory
}
