// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant ADDR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001004;

IAddr constant ADDR_CONTRACT = IAddr(
    ADDR_PRECOMPILE_ADDRESS
);

interface IAddr {
    // Transactions
    function associateWithGas(
        string memory v,
        string memory r,
        string memory s,
        string memory customMessage
    ) external returns (AddrPair associated);

    // Queries
    function getSeiAddr(address addr) external view returns (string memory response);
    function getEvmAddr(string memory addr) external view returns (address response);

    struct AddrPair {
        string seiAddr;
        address evmAddr;
    }
}
