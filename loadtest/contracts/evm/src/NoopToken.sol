// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract NoopToken is ERC20 {

    // this exists just to have a side effect
    uint256 public lastValue;

    constructor(string memory name, string memory symbol) ERC20(name, symbol) {}

    // Override balanceOf to return a maximum balance
    function balanceOf(address) public view virtual override returns (uint256) {
        return type(uint256).max;
    }

    // Override transfer to only emit an event
    function transfer(address recipient, uint256 amount) public virtual override returns (bool) {
        emit Transfer(_msgSender(), recipient, amount);
        lastValue = amount;
        return true;
    }

    // Override transferFrom to only emit an event
    function transferFrom(address sender, address recipient, uint256 amount) public virtual override returns (bool) {
        emit Transfer(sender, recipient, amount);
        lastValue = amount;
        return true;
    }

    // Override approve to only emit an event
    function approve(address spender, uint256 amount) public virtual override returns (bool) {
        emit Approval(_msgSender(), spender, amount);
        return true;
    }

    // Override allowance to return a maximum allowance
    function allowance(address, address) public view virtual override returns (uint256) {
        return type(uint256).max;
    }

    // Override totalSupply to return a fixed supply or max value for simulation
    function totalSupply() public view virtual override returns (uint256) {
        return type(uint256).max;
    }
}
