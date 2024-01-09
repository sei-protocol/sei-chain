// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;

IJson constant JSON_CONTRACT = IJson(
    JSON_PRECOMPILE_ADDRESS
);

interface IJson {
    // Queries
    function extractAsBytes(bytes memory input, string memory key) external view returns (bytes memory response);

    function extractAsBytesList(bytes memory input, string memory key) external view returns (bytes[] memory response);

    function extractAsUint256(bytes memory input, string memory key) external view returns (uint256 response);
}
