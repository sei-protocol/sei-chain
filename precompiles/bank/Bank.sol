// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant BANK_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001001;

IBank constant BANK_CONTRACT = IBank(
    BANK_PRECOMPILE_ADDRESS
);

interface IBank {
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

    struct Coin {
        uint256 amount;
        string denom;
    }

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

    function spendableBalances(
        address acc,
        bytes memory pageKey
    ) external view returns (Coin[] memory balances, bytes memory nextKey);

    function totalSupply(
        bytes memory pageKey
    ) external view returns (Coin[] memory supply, bytes memory nextKey);

    struct SendEnabled {
        string denom;
        bool enabled;
    }

    struct Params {
        SendEnabled[] sendEnabled;
        bool defaultSendEnabled;
    }

    function params() external view returns (Params memory params);

    struct DenomUnit {
        string denom;
        uint32 exponent;
        string[] aliases;
    }

    struct Metadata {
        string description;
        DenomUnit[] denomUnits;
        string base;
        string display;
        string name;
        string symbol;
    }

    function denomMetadata(
        string memory denom
    ) external view returns (Metadata memory metadata);

    function denomsMetadata(
        bytes memory pageKey
    ) external view returns (Metadata[] memory metadatas, bytes memory nextKey);
}
