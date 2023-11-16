// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IBank {
    // Transactions
    function send(
        address fromAddress,
        address toAddress,
        string memory denom,
        uint256 amount
    ) external returns (bool success);

    // Queries
    function balance(
        address acc,
        string memory denom
    ) external view returns (uint256 amount);

    function name(
        string memory denom
    ) external view returns (string memory response);

    function symbol(
        string memory denom
    ) external view returns (string memory response);

    function decimals(
        string memory denom
    ) external view returns (uint8 response);

    function supply(
        string memory denom
    ) external view returns (uint256 response);
}
