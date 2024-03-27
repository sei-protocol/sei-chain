// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {IBank} from "./precompiles/IBank.sol";

contract NativeSeiTokensERC20 is ERC20 {

    address constant BANK_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001001;

    string public denom;
    string public nname;
    string public ssymbol;
    uint8 public ddecimals;
    IBank public BankPrecompile;

    constructor(string memory denom_, string memory name_, string memory symbol_, uint8 decimals_) ERC20("", "") {
        BankPrecompile = IBank(BANK_PRECOMPILE_ADDRESS);
        denom = denom_;
        nname = name_;
        ssymbol = symbol_;
        ddecimals = decimals_;
    }

    function name() public view override returns (string memory) {
        return nname;
    }

    function symbol() public view override returns (string memory) {
        return ssymbol;
    }

    function balanceOf(address account) public view override returns (uint256) {
        return BankPrecompile.balance(account, denom);
    }

    function decimals() public view override returns (uint8) {
        return ddecimals;
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
