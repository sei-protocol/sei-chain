// SPDX-License-Identifier: MIT
pragma solidity ^0.8.12;

import "@openzeppelin/contracts/token/common/ERC2981.sol";
import "@openzeppelin/contracts/token/ERC1155/IERC1155.sol";
import "@openzeppelin/contracts/token/ERC1155/ERC1155.sol";
import "@openzeppelin/contracts/token/ERC1155/extensions/IERC1155MetadataURI.sol";
import "@openzeppelin/contracts/token/ERC1155/IERC1155Receiver.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import "@openzeppelin/contracts/utils/Strings.sol";
import {IERC165} from "@openzeppelin/contracts/utils/introspection/IERC165.sol";
import {IWasmd} from "./precompiles/IWasmd.sol";
import {IJson} from "./precompiles/IJson.sol";
import {IAddr} from "./precompiles/IAddr.sol";


contract CW1155ERC1155Pointer is ERC1155, ERC2981, ReentrancyGuard {

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

    // 2981
    function royaltyInfo(uint256 tokenId, uint256 salePrice) public view override returns (address, uint256) {
        bytes memory checkRoyaltyResponse = WasmdPrecompile.query(Cw1155Address, bytes("{\"extension\":{\"msg\":{\"check_royalties\":{}}}}"));
        bytes memory isRoyaltyImplemented = JsonPrecompile.extractAsBytes(checkRoyaltyResponse, "royalty_payments");
        if (keccak256(isRoyaltyImplemented) != keccak256("true")) {
            revert NotImplementedOnCosmwasmContract("royalty_info");
        }
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(tokenId)));
        string memory sPrice = _formatPayload("sale_price", _doubleQuotes(Strings.toString(salePrice)));
        string memory req = _curlyBrace(_formatPayload("royalty_info", _curlyBrace(_join(tId, sPrice, ","))));
        string memory fullReq = _curlyBrace(_formatPayload("extension", _curlyBrace(_formatPayload("msg", req))));
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes(fullReq));
        bytes memory addr = JsonPrecompile.extractAsBytes(response, "address");
        uint256 amt = JsonPrecompile.extractAsUint256(response, "royalty_amount");
        if (addr.length == 0) {
            return (address(0), amt);
        }
        return (AddrPrecompile.getEvmAddr(string(addr)), amt);
    }

    //transactions
    function safeTransferFrom(address from, address to, uint256 id, uint256 amount, bytes memory data) public override nonReentrant {
        require(to != address(0), "ERC1155: transfer to the zero address");
        require(balanceOf(from, id) >= amount, "ERC1155: insufficient balance for transfer");
        require(msg.sender == from || isApprovedForAll(from, msg.sender), "ERC1155: caller is not approved to transfer");
        if (to.code.length > 0) {
            require(
                IERC1155Receiver(to).onERC1155Received(
                    msg.sender, from, id, amount, data
                ) == IERC1155Receiver.onERC1155Received.selector,
                "unsafe transfer"
            );
        }
    
        string memory f = _formatPayload("from", _doubleQuotes(AddrPrecompile.getSeiAddr(from)));
        string memory t = _formatPayload("to", _doubleQuotes(AddrPrecompile.getSeiAddr(to)));
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(id)));
        string memory amt = _formatPayload("amount", _doubleQuotes(Strings.toString(amount)));
  
        string memory req = _curlyBrace(_formatPayload("send",(_curlyBrace(_join(f,",",_join(t,",",_join(tId,",",amt)))))));
        _execute(bytes(req));
        emit TransferSingle(msg.sender, from, to, id, amount);
    }

    function safeBatchTransferFrom(address from, address to, uint256[] memory ids, uint256[] memory amounts, bytes memory data) public override nonReentrant{
        require(to != address(0), "ERC1155: transfer to the zero address");
        require(isApprovedForAll(from, address(this)), "ERC1155: caller is not approved to transfer");
        require(ids.length == amounts.length, "ERC1155: ids and amounts length mismatch");
        if (to.code.length > 0) {
            require(
                IERC1155Receiver(to).onERC1155BatchReceived(
                    msg.sender, from, ids, amounts, data
                ) == IERC1155Receiver.onERC1155Received.selector,
                "unsafe transfer"
            );
        }
        
        for(uint256 i = 0; i < ids.length; i++){
            require(balanceOf(from, ids[i]) >= amounts[i], "ERC1155: insufficient balance for transfer");
        }
        
        string memory f = _formatPayload("from", _doubleQuotes(AddrPrecompile.getSeiAddr(from)));
        string memory t = _formatPayload("to", _doubleQuotes(AddrPrecompile.getSeiAddr(to)));
        string memory tokenAmount = _formatPayload("token_amount", "[");
        for(uint256 i = 0; i < ids.length; i++){
            string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(ids[i])));
            string memory amt = _formatPayload("amount", _doubleQuotes(Strings.toString(amounts[i])));
            string.concat(tokenAmount, _curlyBrace(_join(tId,",",amt)));
        }
        string.concat(tokenAmount, "]");
        string memory req = _curlyBrace(_formatPayload("send_batch",(_curlyBrace(_join(f,",",_join(t,",",tokenAmount))))));

        _execute(bytes(req));
        emit TransferBatch(msg.sender, from, to, ids, amounts);
    }

    function setApprovalForAll(address operator, bool approved) public override {
        string memory op = _curlyBrace(_formatPayload("operator", _doubleQuotes(AddrPrecompile.getSeiAddr(operator))));
        if (approved) {
            _execute(bytes(_curlyBrace(_formatPayload("approve_all", op))));
        } else {
            _execute(bytes(_curlyBrace(_formatPayload("revoke_all", op))));
        }
        emit ApprovalForAll(msg.sender, operator, approved);
    }

    function _execute(bytes memory req) internal returns (bytes memory) {
        (bool success, bytes memory ret) = WASMD_PRECOMPILE_ADDRESS.delegatecall(
            abi.encodeWithSignature(
                "execute(string,bytes,bytes)",
                Cw1155Address,
                bytes(req),
                bytes("[]")
            )
        );
        require(success, "CosmWasm execute failed");
        return ret;
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
