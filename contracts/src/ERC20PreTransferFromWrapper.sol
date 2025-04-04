// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

// This contract permits testing the case where where app add cowasm logs to and
// existing EVM transaction that emitted a log before the post tx hook is invoked.
contract ERC20PreTransferFromWrapper {
    IERC20 public wrapped;

    event PreTransferFrom(
        address indexed from,
        address indexed to,
        uint256 amount
    );

    constructor(address wrapped_) {
        wrapped = IERC20(wrapped_);
    }

    function transferFrom(
        address from,
        address to,
        uint256 amount
    ) public returns (bool) {
        emit PreTransferFrom(from, to, amount);
        require(wrapped.transferFrom(from, to, amount), "Transfer from failed");
        return true;
    }
}
