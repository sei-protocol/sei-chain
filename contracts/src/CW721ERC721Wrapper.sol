// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/utils/Strings.sol";
import {IWasmd} from "./precompiles/IWasmd.sol";
import {IJson} from "./precompiles/IJson.sol";

contract CW721ERC721Wrapper is ERC721 {

    address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;
    address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;

    string public Cw721Address;
    IWasmd public WasmdPrecompile;
    IJson public JsonPrecompile;

    constructor(string memory Cw721Address_, string memory name_, string memory symbol_) ERC721(name_, symbol_) {
        BankPrecompile = IWasmd(WASMD_PRECOMPILE_ADDRESS);
        JsonPrecompile = IJson(JSON_PRECOMPILE_ADDRESS);
        Cw721Address = Cw721Address_;
    }

    function balanceOf(address owner) public view override returns (uint256) {
        if (owner == address(0)) {
            revert ERC721InvalidOwner(address(0));
        }
        string req = string.concat(string.concat("{\"tokens\":{\"owner\":\"", string(owner)), "\"}}");
        bytes response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes[] tokens = JsonPrecompile.extractAsBytesList(response, "tokens");
        return tokens.length;
    }

    function transferFrom(address from, address to, uint256 tokenId) public override {
        if (to == address(0)) {
            revert ERC721InvalidReceiver(address(0));
        }
        string recipient = string.concat(string.concat("\"recipient\":\"", string(to)),"\"");
        string tId = string.concat(string.concat("\"token_id\":\"", Strings.toString(tokenId)),"\"");
        string req = string.concat(
            string.concat(
                string.concat("{\"transfer_nft\":{", recipient),
                string.concat(",",tId)
            ),
            "}"
        );
        WasmdPrecompile.execute(Cw721Address, string(from), bytes(req), bytes("[]"));
    }
}