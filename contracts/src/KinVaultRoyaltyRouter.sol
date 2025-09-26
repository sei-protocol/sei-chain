// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
}

contract KinVaultRoyaltyRouter {
    address public immutable royaltyReceiver;
    uint256 public constant ROYALTY_BPS = 850; // 8.5%

    event Routed(address indexed user, address token, uint256 amountAfterRoyalty, uint256 royaltyAmount);

    constructor(address _royaltyReceiver) {
        royaltyReceiver = _royaltyReceiver;
    }

    function route(address token, uint256 totalAmount, address recipient) external {
        uint256 royaltyAmount = (totalAmount * ROYALTY_BPS) / 10_000;
        uint256 amountAfter = totalAmount - royaltyAmount;

        require(IERC20(token).transfer(royaltyReceiver, royaltyAmount), "Royalty transfer failed");
        require(IERC20(token).transfer(recipient, amountAfter), "Recipient transfer failed");

        emit Routed(recipient, token, amountAfter, royaltyAmount);
    }
}
