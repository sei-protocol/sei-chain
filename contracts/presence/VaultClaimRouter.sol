// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

contract VaultClaimRouter is Ownable {
    IERC20 public immutable token;
    mapping(address => bool) public verified;

    constructor(address tokenAddress) {
        token = IERC20(tokenAddress);
    }

    function markVerified(address user) external onlyOwner {
        verified[user] = true;
    }

    function claim() external {
        require(verified[msg.sender], "Not verified");
        verified[msg.sender] = false;
        require(token.transfer(msg.sender, 1 ether), "Transfer failed");
    }
}
