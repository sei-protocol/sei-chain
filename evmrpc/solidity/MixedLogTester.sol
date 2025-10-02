// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/utils/Strings.sol";

import {IWasmd} from "./IWasmd.sol";
import {IJson} from "./IJson.sol";
import {IAddr} from "./IAddr.sol";

contract MixedLogTester {
    event  Transfer(address indexed src, address indexed dst, uint256 wad);

    address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;
    address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;
    address constant ADDR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001004;

    string public Cw20Address;
    IWasmd public WasmdPrecompile;
    IJson public JsonPrecompile;
    IAddr public AddrPrecompile;

    constructor(string memory Cw20Address_) {
        WasmdPrecompile = IWasmd(WASMD_PRECOMPILE_ADDRESS);
        JsonPrecompile = IJson(JSON_PRECOMPILE_ADDRESS);
        AddrPrecompile = IAddr(ADDR_PRECOMPILE_ADDRESS);
        Cw20Address = Cw20Address_;
    }

    function transfer(address to, uint256 amount) public returns (bool) {
        require(to != address(0), "ERC20: transfer to the zero address");
        emit Transfer(msg.sender, to, amount);
        string memory recipient = _formatPayload("recipient", _doubleQuotes(AddrPrecompile.getSeiAddr(to)));
        string memory amt = _formatPayload("amount", _doubleQuotes(Strings.toString(amount)));
        string memory req = _curlyBrace(_formatPayload("transfer", _curlyBrace(_join(recipient, amt, ","))));
        _execute(bytes(req));
        return true;
    }

    function _execute(bytes memory req) internal returns (bytes memory) {
        (bool success, bytes memory ret) = WASMD_PRECOMPILE_ADDRESS.call(
            abi.encodeWithSignature(
                "execute(string,bytes,bytes)",
                Cw20Address,
                bytes(req),
                bytes("[]")
            )
        );
        require(success, "CosmWasm execute failed");
        return ret;
    }

    function _formatPayload(string memory key, string memory value) internal pure returns (string memory) {
        return _join(_doubleQuotes(key), value, ":");
    }

    function _curlyBrace(string memory s) internal pure returns (string memory) {
        return string.concat("{", string.concat(s, "}"));
    }

    function _doubleQuotes(string memory s) internal pure returns (string memory) {
        return string.concat("\"", string.concat(s, "\""));
    }

    function _join(string memory a, string memory b, string memory separator) internal pure returns (string memory) {
        return string.concat(a, string.concat(separator, b));
    }
}
