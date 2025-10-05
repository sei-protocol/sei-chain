// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

interface IERC20 {
    function balanceOf(address account) external view returns (uint256);

    function transfer(address recipient, uint256 amount) external returns (bool);
}

contract RoyaltyReceiver {
    address public owner;
    uint256 public totalClaimed;

    event RoyaltyClaimed(address indexed sender, address indexed token, uint256 amount);

    constructor(address _owner) {
        owner = _owner;
    }

    function claim(address token) external {
        uint256 balance = IERC20(token).balanceOf(address(this));
        require(balance > 0, "Nothing to claim");

        totalClaimed += balance;
        emit RoyaltyClaimed(msg.sender, token, balance);

        IERC20(token).transfer(owner, balance);
    }
}
