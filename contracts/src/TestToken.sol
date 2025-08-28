// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract TestToken is ERC20, Ownable {
    constructor(string memory name, string memory symbol) Ownable(msg.sender) ERC20(name, symbol) {
        _mint(msg.sender, 1000 * (10 ** uint256(decimals())));
    }

    // setBalance verifies modifier works
    function setBalance(address account, uint256 amount) public onlyOwner {
        uint256 currentBalance = balanceOf(account);
        if (amount > currentBalance) {
            _mint(account, amount - currentBalance);
        } else if (amount < currentBalance) {
            _burn(account, currentBalance - amount);
        }
    }
}
