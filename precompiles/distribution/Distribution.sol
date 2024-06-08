// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant DISTR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001007;

IDistr constant DISTR_CONTRACT = IDistr(
    DISTR_PRECOMPILE_ADDRESS
);

interface IDistr {
    // Transactions
    function setWithdrawAddress(address withdrawAddr) external returns (bool success);

    function withdrawDelegationRewards(string memory validator) external returns (bool success);

    function withdrawMultipleDelegationRewards(string[] memory validators) external returns (bool success);
}
