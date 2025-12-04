// SPDX-License-Identifier: MIT
pragma solidity ^0.8.12;

import "@openzeppelin/contracts/token/common/ERC2981.sol";
import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import "@openzeppelin/contracts/utils/Strings.sol";
import {IERC165} from "@openzeppelin/contracts/utils/introspection/IERC165.sol";
import {IWasmd} from "./precompiles/IWasmd.sol";
import {IJson} from "./precompiles/IJson.sol";
import {IAddr} from "./precompiles/IAddr.sol";

contract CW721ERC721Pointer is ERC721,ERC2981 {

    address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;
    address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;
    address constant ADDR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001004;

    string public Cw721Address;
    IWasmd public WasmdPrecompile;
    IJson public JsonPrecompile;
    IAddr public AddrPrecompile;

    error NotImplementedOnCosmwasmContract(string method);
    error NotImplemented(string method);

    constructor(string memory Cw721Address_, string memory name_, string memory symbol_) ERC721(name_, symbol_) {
        WasmdPrecompile = IWasmd(WASMD_PRECOMPILE_ADDRESS);
        JsonPrecompile = IJson(JSON_PRECOMPILE_ADDRESS);
        AddrPrecompile = IAddr(ADDR_PRECOMPILE_ADDRESS);
        Cw721Address = Cw721Address_;
    }

    function supportsInterface(bytes4 interfaceId) public pure override(ERC721, ERC2981) returns (bool) {
        return
            interfaceId == type(IERC2981).interfaceId ||
            interfaceId == type(IERC165).interfaceId ||
            interfaceId == type(IERC721).interfaceId ||
            interfaceId == type(IERC721Metadata).interfaceId;
    }

    // Queries
    // owner of the entire collection, not specific to a token id
    function owner() public view returns (address) {
        string memory req = _curlyBrace(_formatPayload("ownership", "{}"));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes memory owner_bytes = JsonPrecompile.extractAsBytes(response, "owner");
        return AddrPrecompile.getEvmAddr(string(owner_bytes));
    }

    function balanceOf(address owner_) public view override returns (uint256) {
        if (owner_ == address(0)) {
            revert ERC721InvalidOwner(address(0));
        }
        uint256 numTokens = 0;
        string memory startAfter;
        string memory qb = string.concat(
            string.concat("\"limit\":1000,\"owner\":\"", AddrPrecompile.getSeiAddr(owner_)),
            "\""
        );
        bytes32 terminator = keccak256("{\"tokens\":[]}");

        bytes[] memory tokens;
        uint256 tokensLength;
        string memory req = string.concat(string.concat("{\"tokens\":{", qb), "}}");
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        while (keccak256(response) != terminator) {
            tokens = JsonPrecompile.extractAsBytesList(response, "tokens");
            tokensLength = tokens.length;
            numTokens += tokensLength;
            startAfter = string.concat(",\"start_after\":", string(tokens[tokensLength-1]));
            req = string.concat(
                string.concat("{\"tokens\":{", string.concat(qb, startAfter)),
                "}}"
            );
            response = WasmdPrecompile.query(Cw721Address, bytes(req));
        }
        return numTokens;
    }

    function ownerOf(uint256 tokenId) public view override returns (address) {
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory req = _curlyBrace(_formatPayload("owner_of", _curlyBrace(tId)));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes memory owner_ = JsonPrecompile.extractAsBytes(response, "owner");
        return AddrPrecompile.getEvmAddr(string(owner_));
    }

    function getApproved(uint256 tokenId) public view override returns (address) {
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory req = _curlyBrace(_formatPayload("approvals", _curlyBrace(tId)));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes[] memory approvals = JsonPrecompile.extractAsBytesList(response, "approvals");
        if (approvals.length > 0) {
            bytes memory res = JsonPrecompile.extractAsBytes(approvals[0], "spender");
            return AddrPrecompile.getEvmAddr(string(res));
        }
        return address(0);
    }

    function isApprovedForAll(address owner_, address operator) public view override returns (bool) {
        string memory o = _formatPayload("owner", _doubleQuotes(AddrPrecompile.getSeiAddr(owner_)));
        string memory req = _curlyBrace(_formatPayload("all_operators", _curlyBrace(o)));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes[] memory approvals = JsonPrecompile.extractAsBytesList(response, "operators");
        for (uint i=0; i<approvals.length; i++) {
            bytes memory op = JsonPrecompile.extractAsBytes(approvals[i], "spender");
            if (AddrPrecompile.getEvmAddr(string(op)) == operator) {
                return true;
            }
        }
        return false;
    }

    // 2981
    function royaltyInfo(uint256 tokenId, uint256 salePrice) public view override returns (address, uint256) {
        bytes memory checkRoyaltyResponse = WasmdPrecompile.query(Cw721Address, bytes("{\"extension\":{\"msg\":{\"check_royalties\":{}}}}"));
        bytes memory isRoyaltyImplemented = JsonPrecompile.extractAsBytes(checkRoyaltyResponse, "royalty_payments");
        if (keccak256(isRoyaltyImplemented) != keccak256("true")) {
            revert NotImplementedOnCosmwasmContract("royalty_info");
        }
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory sPrice = _formatPayload("sale_price", _doubleQuotes(Strings.toString(salePrice)));
        string memory req = _curlyBrace(_formatPayload("royalty_info", _curlyBrace(_join(tId, sPrice, ","))));
        string memory fullReq = _curlyBrace(_formatPayload("extension", _curlyBrace(_formatPayload("msg", req))));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(fullReq));
        bytes memory addr = JsonPrecompile.extractAsBytes(response, "address");
        uint256 amt = JsonPrecompile.extractAsUint256(response, "royalty_amount");
        if (addr.length == 0) {
            return (address(0), amt);
        }
        return (AddrPrecompile.getEvmAddr(string(addr)), amt);
    }

    function tokenURI(uint256 tokenId) public view override returns (string memory) {
        // revert if token isn't owned
        ownerOf(tokenId);
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory req = _curlyBrace(_formatPayload("nft_info", _curlyBrace(tId)));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes memory uri = JsonPrecompile.extractAsBytes(response, "token_uri");
        return string(uri);
    }

    // 721-Enumerable
    function totalSupply() public view virtual returns (uint256) {
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes("{\"num_tokens\":{}}"));
        return JsonPrecompile.extractAsUint256(response, "count");
    }

    function tokenOfOwnerByIndex(address, uint256) public view virtual returns (uint256) {
        revert NotImplemented("tokenOfOwnerByIndex");
    }

    function tokenByIndex(uint256) public view virtual returns (uint256) {
        revert NotImplemented("tokenByIndex");
    }

    // Transactions
    function transferFrom(address from, address to, uint256 tokenId) public override {
        if (to == address(0)) {
            revert ERC721InvalidReceiver(address(0));
        }
        require(from == ownerOf(tokenId), "`from` must be the owner");
        string memory recipient = _formatPayload("recipient", _doubleQuotes(AddrPrecompile.getSeiAddr(to)));
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory req = _curlyBrace(_formatPayload("transfer_nft", _curlyBrace(_join(recipient, tId, ","))));
        _execute(bytes(req));
    }

    function approve(address approved, uint256 tokenId) public override {
        string memory spender = _formatPayload("spender", _doubleQuotes(AddrPrecompile.getSeiAddr(approved)));
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory req = _curlyBrace(_formatPayload("approve", _curlyBrace(_join(spender, tId, ","))));
        _execute(bytes(req));
    }

    function setApprovalForAll(address operator, bool approved) public override {
        string memory op = _curlyBrace(_formatPayload("operator", _doubleQuotes(AddrPrecompile.getSeiAddr(operator))));
        if (approved) {
            _execute(bytes(_curlyBrace(_formatPayload("approve_all", op))));
        } else {
            _execute(bytes(_curlyBrace(_formatPayload("revoke_all", op))));
        }
    }

    function _execute(bytes memory req) internal returns (bytes memory) {
        (bool success, bytes memory ret) = WASMD_PRECOMPILE_ADDRESS.delegatecall(
            abi.encodeWithSignature(
                "execute(string,bytes,bytes)",
                Cw721Address,
                bytes(req),
                bytes("[]")
            )
        );
        require(success, "CosmWasm execute failed");
        return ret;
    }

    function _queryContractInfo() internal view virtual returns (string memory, string memory) {
        string memory req = _curlyBrace(_formatPayload("contract_info", "{}"));
        bytes memory response = WasmdPrecompile.query(Cw721Address, bytes(req));
        bytes memory respName = JsonPrecompile.extractAsBytes(response, "name");
        bytes memory respSymbol = JsonPrecompile.extractAsBytes(response, "symbol");
        string memory nameStr = string(respName);
        string memory symbolStr = string(respSymbol);
        return (nameStr, symbolStr);
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