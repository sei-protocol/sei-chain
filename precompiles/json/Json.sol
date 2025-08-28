// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;

IJson constant JSON_CONTRACT = IJson(
    JSON_PRECOMPILE_ADDRESS
);

/**
 * @dev Interface for interacting with the JSON precompile contract.
 */
interface IJson {
    /**
     * @dev Extracts a value as bytes from the JSON input using the specified key.
     * @param input The JSON input as bytes.
     * @param key The key to extract the value for.
     * @return response The extracted value as bytes.
     */
    function extractAsBytes(bytes memory input, string memory key) external view returns (bytes memory response);

    /**
     * @dev Extracts a list of values as bytes from the JSON input using the specified key.
     * @param input The JSON input as bytes.
     * @param key The key to extract the values for.
     * @return response The extracted values as a list of bytes.
     */
    function extractAsBytesList(bytes memory input, string memory key) external view returns (bytes[] memory response);

    /**
     * @dev Extracts a value as uint256 from the JSON input using the specified key.
     * @param input The JSON input as bytes.
     * @param key The key to extract the value for.
     * @return response The extracted value as uint256.
     */
    function extractAsUint256(bytes memory input, string memory key) external view returns (uint256 response);

    /**
     * @dev Extracts a value as bytes from an array in the JSON input using the specified index.
     * @param input The JSON array as bytes.
     * @param arrayIndex The index in the array to extract the value from.
     * @return response The extracted value as bytes.
     */
    function extractAsBytesFromArray(bytes memory input, uint16 memory arrayIndex) external view returns (bytes memory response);
}
