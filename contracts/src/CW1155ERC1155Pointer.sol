// SPDX-License-Identifier: MIT
pragma solidity ^0.8.12;

import "@openzeppelin/contracts/token/common/ERC2981.sol";
import "@openzeppelin/contracts/token/ERC1155/IERC1155.sol";
import "@openzeppelin/contracts/token/ERC1155/ERC1155.sol";
import "@openzeppelin/contracts/token/ERC1155/extensions/IERC1155MetadataURI.sol";
import "@openzeppelin/contracts/utils/Strings.sol";
import {IERC165} from "@openzeppelin/contracts/utils/introspection/IERC165.sol";
import {IWasmd} from "./precompiles/IWasmd.sol";
import {IJson} from "./precompiles/IJson.sol";
import {IAddr} from "./precompiles/IAddr.sol";


contract CW1155ERC1155Pointer is ERC1155, ERC2981 {

    address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;
    address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;
    address constant ADDR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001004;

    string public Cw1155Address;
    IWasmd public WasmdPrecompile;
    IJson public JsonPrecompile;
    IAddr public AddrPrecompile;

    error NotImplementedOnCosmwasmContract(string method);
    error NotImplemented(string method);

    constructor(string memory Cw1155Address_, string memory uri_) ERC1155(uri_) {
        WasmdPrecompile = IWasmd(WASMD_PRECOMPILE_ADDRESS);
        JsonPrecompile = IJson(JSON_PRECOMPILE_ADDRESS);
        AddrPrecompile = IAddr(ADDR_PRECOMPILE_ADDRESS);
        Cw1155Address = Cw1155Address_;
    }

    function supportsInterface(bytes4 interfaceId) public pure override(ERC2981, ERC1155) returns (bool) {
        return
            interfaceId == type(IERC2981).interfaceId ||
            interfaceId == type(IERC1155).interfaceId ||
            interfaceId == type(IERC1155MetadataURI).interfaceId ||
            interfaceId == type(IERC165).interfaceId;
    }

    // Queries
    function balanceOf(address account, uint256 id) public view override returns (uint256) {
        require(account != address(0), "ERC1155: cannot query balance of zero address");
        string memory own = _formatPayload("owner", _doubleQuotes(AddrPrecompile.getSeiAddr(account)));
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(id)));
        string memory req = _curlyBrace(_formatPayload("balance_of",(_curlyBrace(_join(own,",",tId)))));
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes(req));
        return JsonPrecompile.extractAsUint256(response, "balance");
    }

    function balanceOfBatch(address[] memory accounts, uint256[] memory ids) public view override returns (uint256[] memory balances){
        if(accounts.length != ids.length){
            revert ERC1155InvalidArrayLength(ids.length, accounts.length);
        }
        balances = new uint256[](accounts.length);
        for(uint256 i = 0; i < accounts.length; i++){
            balances[i] = balanceOf(accounts[i], ids[i]);
        }
    }

    function uri(uint256 id) public view override returns (string memory) {
        string memory tId = _curlyBrace(_formatPayload("token_id", _doubleQuotes(Strings.toString(id))));
        string memory req = _curlyBrace(_formatPayload("token_info",(tId)));
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes(req));
        return string(JsonPrecompile.extractAsBytes(response, "token_uri"));
    }

    function isApprovedForAll(address owner, address operator) public view override returns (bool) {
        string memory own = _formatPayload("owner", _doubleQuotes(AddrPrecompile.getSeiAddr(owner)));
        string memory op = _formatPayload("operator", _doubleQuotes(AddrPrecompile.getSeiAddr(operator)));
        string memory req = _curlyBrace(_formatPayload("is_approved_for_all",(_curlyBrace(_join(own,",",op)))));
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes(req));
        return JsonPrecompile.extractAsUint256(response, "approved") == 1;
    }


    function _formatPayload(string memory key, string memory value) internal pure returns (string memory) {
        return _join(_doubleQuotes(key), ":", value);
    }

    function _curlyBrace(string memory s) internal pure returns (string memory) {
        return string.concat("{", string.concat(s, "}"));
    }

    function _doubleQuotes(string memory s) internal pure returns (string memory) {
        return string.concat("\"", string.concat(s, "\""));
    }

    function _join(string memory a, string memory separator, string memory b) internal pure returns (string memory) {
        return string.concat(a, string.concat(separator, b));
    }
}
