// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract VaultClaimRouter {
    address public token;
    mapping(address => bool) public claimed;

    event TokensClaimed(address indexed user, uint256 amount);

    constructor(address _token) {
        token = _token;
    }

    function claimPresenceReward(address user, uint256 amount) external {
        require(!claimed[user], "Already claimed");
        claimed[user] = true;

        (bool success, ) = token.call(
            abi.encodeWithSignature("transfer(address,uint256)", user, amount)
        );
        require(success, "Transfer failed");

        emit TokensClaimed(user, amount);
    }
}
