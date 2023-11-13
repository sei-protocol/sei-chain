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
        string memory creator,
        string memory admin,
        bytes memory msg,
        string memory label,
        bytes memory coins
    ) external returns (string memory contractAddr, bytes memory data);

    function execute(
        string memory contractAddress,
        string memory sender,
        bytes memory msg,
        bytes memory coins
    ) external returns (bytes memory response);

    // Queries
    function query(string memory contractAddress, bytes memory req) external view returns (bytes memory response);
}
