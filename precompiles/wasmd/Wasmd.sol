// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;

IWasmd constant WASMD_CONTRACT = IWasmd(
    WASMD_PRECOMPILE_ADDRESS
);

interface IWasmd {
    // Transactions
    function instantiate(
        uint64 codeID,
        string memory admin,
        bytes memory msg,
        string memory label,
        bytes memory coins
    ) payable external returns (string memory contractAddr, bytes memory data);

    function execute(
        string memory contractAddress,
        bytes memory msg,
        bytes memory coins
    ) payable external returns (bytes memory response);

    struct ExecuteMsg {
        string contractAddress;
        bytes msg;
        bytes coins;
    }

    function execute_batch(ExecuteMsg[] memory executeMsgs) payable external returns (bytes[] memory responses);

    // Queries
    function query(string memory contractAddress, bytes memory req) external view returns (bytes memory response);

    /// @notice Gets metadata about a contract instance
    /// @param contractAddress The contract's Sei address
    /// @return info The contract's metadata
    function contractInfo(string memory contractAddress) external view returns (ContractInfo memory info);

    /// @notice Gets the code history of a contract instance
    /// @param contractAddress The contract's Sei address
    /// @param pageKey The pagination key from a previous response (empty bytes for the first page)
    /// @return entries The contract's code history entries
    /// @return nextKey The pagination key for the next page (empty when exhausted)
    function contractHistory(string memory contractAddress, bytes memory pageKey) external view returns (ContractCodeHistoryEntry[] memory entries, bytes memory nextKey);

    /// @notice Lists the contracts instantiated from a code id
    /// @param codeID The stored code id
    /// @param pageKey The pagination key from a previous response (empty bytes for the first page)
    /// @return contracts The Sei addresses of the contracts instantiated from codeID
    /// @return nextKey The pagination key for the next page (empty when exhausted)
    function contractsByCode(uint64 codeID, bytes memory pageKey) external view returns (string[] memory contracts, bytes memory nextKey);

    /// @notice Gets all raw store data for a contract
    /// @param contractAddress The contract's Sei address
    /// @param pageKey The pagination key from a previous response (empty bytes for the first page)
    /// @return models The contract's raw key-value store entries
    /// @return nextKey The pagination key for the next page (empty when exhausted)
    function allContractState(string memory contractAddress, bytes memory pageKey) external view returns (Model[] memory models, bytes memory nextKey);

    /// @notice Gets a single raw store value for a contract
    /// @param contractAddress The contract's Sei address
    /// @param queryData The raw store key to read
    /// @return data The raw store value, empty if the key is unset
    function rawContractState(string memory contractAddress, bytes memory queryData) external view returns (bytes memory data);

    /// @notice Gets the metadata and binary of a stored wasm code
    /// @param codeID The stored code id
    /// @return info The code's metadata
    /// @return data The code's wasm binary
    function code(uint64 codeID) external view returns (CodeInfo memory info, bytes memory data);

    /// @notice Lists the metadata for all stored wasm codes
    /// @param pageKey The pagination key from a previous response (empty bytes for the first page)
    /// @return codeInfos The metadata of every stored code
    /// @return nextKey The pagination key for the next page (empty when exhausted)
    function codes(bytes memory pageKey) external view returns (CodeInfo[] memory codeInfos, bytes memory nextKey);

    /// @notice Lists the code ids pinned in the wasmvm cache
    /// @param pageKey The pagination key from a previous response (empty bytes for the first page)
    /// @return codeIDs The pinned code ids
    /// @return nextKey The pagination key for the next page (empty when exhausted)
    function pinnedCodes(bytes memory pageKey) external view returns (uint64[] memory codeIDs, bytes memory nextKey);

    /// @notice Metadata about a contract instance
    struct ContractInfo {
        /// @notice The code id the contract was instantiated from
        uint64 codeID;
        /// @notice The Sei address that instantiated the contract
        string creator;
        /// @notice The Sei address allowed to migrate the contract, empty if none
        string admin;
        /// @notice Metadata stored with the contract instance
        string label;
        /// @notice The contract's IBC port id, empty if it does not implement IBC
        string ibcPortID;
    }

    /// @notice A single entry in a contract's code history
    struct ContractCodeHistoryEntry {
        /// @notice The operation that produced this entry (init, migrate, or genesis)
        uint8 operation;
        /// @notice The code id in effect after this entry
        uint64 codeID;
        /// @notice The message passed to the operation that produced this entry
        bytes msg;
    }

    /// @notice A single key-value pair from a contract's raw store
    struct Model {
        /// @notice The raw store key
        bytes key;
        /// @notice The raw store value
        bytes value;
    }

    /// @notice Access control for an operation such as code upload or instantiation
    struct AccessConfig {
        /// @notice The permission type (unspecified, nobody, only address, or everybody)
        uint8 permission;
        /// @notice The Sei address allowed to perform the operation when permission is "only address"
        string address;
    }

    /// @notice Metadata about a stored wasm code
    struct CodeInfo {
        /// @notice The stored code id
        uint64 codeID;
        /// @notice The Sei address that uploaded the code
        string creator;
        /// @notice The checksum of the wasm binary
        bytes dataHash;
        /// @notice Who is allowed to instantiate this code
        AccessConfig instantiatePermission;
    }
}
