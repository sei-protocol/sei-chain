// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {IBank} from "./precompiles/IBank.sol";
import {console2} from "forge-std/Test.sol";

contract NativeSeiTokensERC20 is ERC20 {

    address constant BANK_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001001;

    string public denom;
    IBank public BankPrecompile;

    constructor(string memory denom_) ERC20("", "") {
        BankPrecompile = IBank(BANK_PRECOMPILE_ADDRESS);
        denom = denom_;
    }

    function name() public view override returns (string memory) {
        return BankPrecompile.name(denom);
    }

    function symbol() public view override returns (string memory) {
        return BankPrecompile.symbol(denom);
    }

    function balanceOf(address account) public view override returns (uint256) {
        return BankPrecompile.balance(account, denom);
    }

    function decimals() public view override returns (uint8) {
        return BankPrecompile.decimals(denom);
    }

    function totalSupply() public view override returns (uint256) {
        return BankPrecompile.supply(denom);
    }

    function _update(address from, address to, uint256 value) internal override {
        bool success = BankPrecompile.send(from, to, denom, value);
        require(success, "NativeSeiTokensERC20: transfer failed");
        emit Transfer(from, to, value);
    }
}
