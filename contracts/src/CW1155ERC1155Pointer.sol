// SPDX-License-Identifier: MIT
pragma solidity ^0.8.12;

import "@openzeppelin/contracts/token/common/ERC2981.sol";
import "@openzeppelin/contracts/token/ERC1155/IERC1155.sol";
import "@openzeppelin/contracts/token/ERC1155/ERC1155.sol";
import "@openzeppelin/contracts/token/ERC1155/extensions/IERC1155MetadataURI.sol";
import "@openzeppelin/contracts/token/ERC1155/IERC1155Receiver.sol";
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
    string public name;
    string public symbol;

    error NotImplementedOnCosmwasmContract(string method);
    error NotImplemented(string method);

    constructor(string memory Cw1155Address_, string memory name_, string memory symbol_) ERC1155("") {
        Cw1155Address = Cw1155Address_;
        WasmdPrecompile = IWasmd(WASMD_PRECOMPILE_ADDRESS);
        JsonPrecompile = IJson(JSON_PRECOMPILE_ADDRESS);
        AddrPrecompile = IAddr(ADDR_PRECOMPILE_ADDRESS);
        name = name_;
        symbol = symbol_;
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
        require(accounts.length != 0, "ERC1155: cannot query empty accounts list");
        if(accounts.length != ids.length){
            revert ERC1155InvalidArrayLength(ids.length, accounts.length);
        }
        string memory ownerTokens = "[";
        for (uint256 i = 0; i < accounts.length; i++) {
            require(accounts[i] != address(0), "ERC1155: cannot query balance of zero address");
            if (i > 0) {
                ownerTokens = string.concat(ownerTokens, ",");
            }
            string memory ownerToken = string.concat("{\"owner\":\"", AddrPrecompile.getSeiAddr(accounts[i]));
            ownerToken = string.concat(ownerToken, "\",\"token_id\":\"");
            ownerToken = string.concat(ownerToken, Strings.toString(ids[i]));
            ownerToken = string.concat(ownerToken, "\"}");
            ownerTokens = string.concat(ownerTokens, ownerToken);
        }
        ownerTokens = string.concat(ownerTokens, "]");
        string memory req = _curlyBrace(_formatPayload("balance_of_batch", ownerTokens));
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes(req));
        bytes[] memory parseResponse = JsonPrecompile.extractAsBytesList(response, "balances");
        require(parseResponse.length == accounts.length, "Invalid balance_of_batch response");
        balances = new uint256[](parseResponse.length);
        for(uint256 i = 0; i < parseResponse.length; i++){
            balances[i] = JsonPrecompile.extractAsUint256(parseResponse[i], "amount");
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
        bytes32 response = keccak256(WasmdPrecompile.query(Cw1155Address, bytes(req)));
        bytes32 approvedMsg = keccak256("{\"approved\":true}");
        bytes32 unapprovedMsg = keccak256("{\"approved\":false}");
        if (response == approvedMsg) {
            return true;
        } else if (response == unapprovedMsg) {
            return false;
        }
        revert NotImplementedOnCosmwasmContract("is_approved_for_all");
    }

    // ERC1155Supply
    function totalSupply() public view virtual returns (uint256) {
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes("{\"num_tokens\":{}}"));
        return JsonPrecompile.extractAsUint256(response, "count");
    }

    function totalSupply(uint256 id) public view virtual returns (uint256) {
        string memory query = string.concat(
            string.concat("{\"num_tokens\":{\"token_id\":\"", Strings.toString(id)),
            "\"}}"
        );
        bytes memory response = WasmdPrecompile.query(Cw1155Address, bytes(query));
        return JsonPrecompile.extractAsUint256(response, "count");
    }

    function exists(uint256 id) public view virtual returns (bool) {
        return totalSupply(id) > 0;
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
        string memory req = _curlyBrace(_formatPayload("royalty_info", _curlyBrace(_join(tId, ",", sPrice))));
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
    function safeTransferFrom(address from, address to, uint256 id, uint256 amount, bytes memory data) public override {
        require(to != address(0), "ERC1155: transfer to the zero address");
        require(balanceOf(from, id) >= amount, "ERC1155: insufficient balance for transfer");
        require(msg.sender == from || isApprovedForAll(from, msg.sender), "ERC1155: caller is not approved to transfer");
    
        string memory f = _formatPayload("from", _doubleQuotes(AddrPrecompile.getSeiAddr(from)));
        string memory t = _formatPayload("to", _doubleQuotes(AddrPrecompile.getSeiAddr(to)));
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(id)));
        string memory amt = _formatPayload("amount", _doubleQuotes(Strings.toString(amount)));
  
        string memory req = _curlyBrace(_formatPayload("send",(_curlyBrace(_join(f,",",_join(t,",",_join(tId,",",amt)))))));
        _execute(bytes(req));
        emit TransferSingle(msg.sender, from, to, id, amount);
        if (to.code.length > 0) {
            require(
                IERC1155Receiver(to).onERC1155Received(
                    msg.sender, from, id, amount, data
                ) == IERC1155Receiver.onERC1155Received.selector,
                "unsafe transfer"
            );
        }
    }

    function safeBatchTransferFrom(address from, address to, uint256[] memory ids, uint256[] memory amounts, bytes memory data) public override {
        require(to != address(0), "ERC1155: transfer to the zero address");
        require(msg.sender == from || isApprovedForAll(from, msg.sender), "ERC1155: caller is not approved to transfer");
        require(ids.length == amounts.length, "ERC1155: ids and amounts length mismatch");
        address[] memory batchFrom = new address[](ids.length);
        for(uint256 i = 0; i < ids.length; i++){
            batchFrom[i] = from;
        }
        uint256[] memory balances = balanceOfBatch(batchFrom, ids);
        for(uint256 i = 0; i < balances.length; i++){
            require(balances[i] >= amounts[i], "ERC1155: insufficient balance for transfer");
        }

        string memory payload = string.concat("{\"send_batch\":{\"from\":\"", AddrPrecompile.getSeiAddr(from));
        payload = string.concat(payload, "\",\"to\":\"");
        payload = string.concat(payload, AddrPrecompile.getSeiAddr(to));
        payload = string.concat(payload, "\",\"batch\":[");
        for(uint256 i = 0; i < ids.length; i++){
            string memory batch = string.concat("{\"token_id\":\"", Strings.toString(ids[i]));
            batch = string.concat(batch, "\",\"amount\":\"");
            batch = string.concat(batch, Strings.toString(amounts[i]));
            if (i < ids.length - 1) {
                batch = string.concat(batch, "\"},");
            } else {
                batch = string.concat(batch, "\"}");
            }
            payload = string.concat(payload, batch);
        }
        payload = string.concat(payload, "]}}");
        _execute(bytes(payload));
        emit TransferBatch(msg.sender, from, to, ids, amounts);
        if (to.code.length > 0) {
            require(
                IERC1155Receiver(to).onERC1155BatchReceived(
                    msg.sender, from, ids, amounts, data
                ) == IERC1155Receiver.onERC1155BatchReceived.selector,
                "unsafe transfer"
            );
        }
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

    // ERC1155Burnable transactions
    function burn(address account, uint256 id, uint256 amount) public virtual {
        require(account != address(0), "ERC1155: cannot burn from the zero address");
        require(balanceOf(account, id) >= amount, "ERC1155: insufficient balance for burning");
        require(
            msg.sender == account || isApprovedForAll(account, msg.sender),
            "ERC1155: caller is not approved to burn"
        );

        string memory f = _formatPayload("from", _doubleQuotes(AddrPrecompile.getSeiAddr(account)));
        string memory tId = _formatPayload("token_id", _doubleQuotes(Strings.toString(id)));
        string memory amt = _formatPayload("amount", _doubleQuotes(Strings.toString(amount)));

        string memory req = _curlyBrace(
            _formatPayload("burn", _curlyBrace(_join(f, ",", _join(tId, ",", amt))))
        );
        _execute(bytes(req));
        emit TransferSingle(msg.sender, account, address(0), id, amount);
    }

    function burnBatch(address account, uint256[] memory ids, uint256[] memory amounts) public virtual {
        require(account != address(0), "ERC1155: cannot burn from the zero address");
        require(
            msg.sender == account || isApprovedForAll(account, msg.sender),
            "ERC1155: caller is not approved to burn"
        );
        require(ids.length == amounts.length, "ERC1155: ids and amounts length mismatch");

        address[] memory batchFrom = new address[](ids.length);
        for (uint256 i = 0; i < ids.length; i++) {
            batchFrom[i] = account;
        }
        uint256[] memory balances = balanceOfBatch(batchFrom, ids);
        for (uint256 i = 0; i < balances.length; i++) {
            require(balances[i] >= amounts[i], "ERC1155: insufficient balance for burning");
        }

        string memory payload = string.concat("{\"burn_batch\":{\"from\":\"", AddrPrecompile.getSeiAddr(account));
        payload = string.concat(payload, "\",\"batch\":[");
        for (uint256 i = 0; i < ids.length; i++) {
            string memory batch = string.concat("{\"token_id\":\"", Strings.toString(ids[i]));
            batch = string.concat(batch, "\",\"amount\":\"");
            batch = string.concat(batch, Strings.toString(amounts[i]));
            if (i < ids.length - 1) {
                batch = string.concat(batch, "\"},");
            } else {
                batch = string.concat(batch, "\"}");
            }
            payload = string.concat(payload, batch);
        }
        payload = string.concat(payload, "]}}");
        _execute(bytes(payload));
        emit TransferBatch(msg.sender, account, address(0), ids, amounts);
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
