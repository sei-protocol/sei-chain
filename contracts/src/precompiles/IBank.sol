// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant BANK_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001001;

IBank constant BANK_CONTRACT = IBank(
    BANK_PRECOMPILE_ADDRESS
);

interface IBank {
    struct Coin {
        uint256 amount;
        string denom;
    }
    // Transactions
    function send(
        address fromAddress,
        address toAddress,
        string memory denom,
        uint256 amount
    ) external returns (bool success);

    function sendNative(
        string memory toNativeAddress
    ) payable external returns (bool success);

    // Queries
    function balance(
        address acc,
        string memory denom
    ) external view returns (uint256 amount);

    function all_balances(
        address acc
    ) external view returns (Coin[] memory response);

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
