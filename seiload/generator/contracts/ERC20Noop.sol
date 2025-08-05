// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ERC20Noop {
    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 public totalSupply;

    uint256 public constant DEFAULT_BALANCE = 1000 * 10**18;
    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    constructor(string memory _name, string memory _symbol) {
        name = _name;
        symbol = _symbol;
        decimals = 18;  // Standard for ERC20 tokens
    }

    function balanceOf(address account) public view returns (uint256) {
        uint256 actualBalance = _balances[account];
        return actualBalance > 0 ? actualBalance : DEFAULT_BALANCE;
    }

    function transfer(address recipient, uint256 amount) public returns (bool) {
        return true;
    }

    function approve(address spender, uint256 amount) public returns (bool) {
        return true;
    }

    function allowance(address owner, address spender) public view returns (uint256) {
        return _allowances[owner][spender];
    }

    function transferFrom(address sender, address recipient, uint256 amount) public returns (bool) {
        return true;
    }

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
}