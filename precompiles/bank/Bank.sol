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

    function balance_for_address(
        string memory acc,
        string memory denom
    ) external view returns (uint256 amount);

    struct Coin {
        uint256 amount;
        string denom;
    }

    function all_balances(
        address acc
    ) external view returns (Coin[] memory response);

    function all_balances_for_address(
        string memory acc
    ) external view returns (Coin[] memory response);

    function spendable_balances(
        address acc
    ) external view returns (Coin[] memory response);

    function spendable_balances_for_address(
        string memory acc
    ) external view returns (Coin[] memory response);

    function total_supply()
        external view returns (Coin[] memory response);

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

    function denom_metadata(
        string memory denom
    ) external view returns (Metadata memory response);

    function denoms_metadata()
        external view returns (Metadata[] memory response);

    function params()
        external view returns (Params memory response);

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

    struct SendEnabled {
        string denom;
        bool enabled;
    }

    struct Params {
        SendEnabled[] sendEnabled;
        bool defaultSendEnabled;
    }
}
