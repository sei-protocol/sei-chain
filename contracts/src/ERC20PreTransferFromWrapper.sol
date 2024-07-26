// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

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
