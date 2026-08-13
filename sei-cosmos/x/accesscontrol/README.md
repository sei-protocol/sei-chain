# x/accesscontrol

The x/accesscontrol module is part of the Sei Protocol on Cosmos. This module is responsible for defining and managing the resource dependencies for each message type in the system to enable concurrent transaction execution.

## Resource Dependency Mapping

The concept of a resource dependency mapping is central to the x/accesscontrol module. These mappings define which resources depend on other resources in the system, and allow users to define read and/or write access operations. By managing these dependencies, the module enables transactions to be executed concurrently in a block while ensuring deterministic results.

The module provides query commands for obtaining these resource dependency mappings. These commands return the current state of resource dependencies, either for a specific resource or for all resources in the system.

There is also a transaction command to update resource dependency mappings through a proposal mechanism. This allows the system's users to suggest changes to the resource dependencies, which can then be accepted or rejected by the system's governance process.

### Wasm Dependency Mapping

In addition to the general resource dependency mappings, the x/accesscontrol module also has specific support for Wasm contract dependencies. This recognizes the fact that Wasm contracts are a key resource in the system and may have specific dependency requirements.

The module provides a query command to get the Wasm contract dependency mapping for a specific contract address. This can be used to inspect the dependencies of a Wasm contract.

There is also a transaction command to register dependencies for a Wasm contract. This allows the dependencies of a contract to be defined and updated as necessary.

### Concurrent Transaction Execution

The x/accesscontrol module's primary function is to enable concurrent transaction execution within a block while maintaining deterministic results. By defining resource dependencies (including Wasm contract dependencies) when messages are added to the system, the module can build a dependency graph for each block. This allows transactions to be executed concurrently, increasing throughput and efficiency.

In summary, the x/accesscontrol module provides a mechanism for managing and enforcing access control in the system through the concept of resource dependencies. It allows for concurrent transaction execution within a block by defining read and write access operations, and maintaining a resource dependency graph for deterministic results.

## Query Commands

The x/accesscontrol module supports various query commands:

Get Params: Returns the parameters for the x/accesscontrol module. Run with: `seid q accesscontrol params`

Get Resource Dependency Mapping: Returns the resource dependency mapping for a specific message key. Run with: `seid q accesscontrol resource-dependency-mapping [messageKey]`.

List Resource Dependency Mapping: Lists all resource dependency mappings. Run with: `seid q accesscontrol list-resource-dependency-mapping `

Transaction Commands
The x/accesscontrol module supports various transaction commands:

Update Resource Dependency Mapping Proposal: Submits a proposal to update resource dependencies between objects. The proposal should be provided as a JSON file with the following structure:

```json
{
  "title": "[title]",
  "description": "[description]",
  "deposit": "[deposit]",
  "message_dependency_mapping": "[<list of message dependency mappings>]"
}
```

Run with: seid tx accesscontrol update-resource-dependency-mapping [proposal-file].

Register Wasm Dependency Mapping: Registers dependencies for a Wasm contract. The mapping should be provided as a JSON file with the following structure:

```json
{
  "wasm_dependency_mapping": "<wasm dependency mapping>"
}
```

Run with: seid tx accesscontrol register-wasm-dependency-mapping [mapping-json-file].
