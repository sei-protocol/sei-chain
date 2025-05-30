// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant STAKING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001005;

IStaking constant STAKING_CONTRACT = IStaking(
    STAKING_PRECOMPILE_ADDRESS
);

interface IStaking {
    // Transactions
    function delegate(
        string memory valAddress
    ) payable external returns (bool success);

    function redelegate(
        string memory srcAddress,
        string memory dstAddress,
        uint256 amount
    ) external returns (bool success);

    function undelegate(
        string memory valAddress,
        uint256 amount
    ) external returns (bool success);

    function createValidator(
        string memory pubKeyHex,
        string memory amount,    // e.g 100000usei
        string memory moniker,
        string memory commissionRate,
        string memory commissionMaxRate,
        string memory commissionMaxChangeRate,
        uint256 memory minSelfDelegation
    ) payable external returns (bool success);

    // Queries
    function delegation(
        address delegator,
        string memory valAddress
    ) external view returns (Delegation delegation);

    struct Delegation {
        Balance balance;
        DelegationDetails delegation;
    }

    struct Balance {
        uint256 amount;
        string denom;
    }

    struct DelegationDetails {
        string delegator_address;
        uint256 shares;
        uint256 decimals;
        string validator_address;
    }
}