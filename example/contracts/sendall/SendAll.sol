// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "../../../precompiles/bank/Bank.sol";

contract SendAll {
    function sendAll(
        address fromAddress,
        address toAddress,
        string memory denom
    ) public {
        uint256 amount = BANK_CONTRACT.balance(fromAddress, denom);
        BANK_CONTRACT.send(fromAddress, toAddress, denom, amount);
    }
}