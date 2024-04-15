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
}
