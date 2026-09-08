// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

contract TestNFT {
    string public constant name = "ReplayNFT";
    string public constant symbol = "RNFT";
    address public immutable owner;
    mapping(uint256 => address) public ownerOf;
    mapping(address => uint256) public balanceOf;
    mapping(uint256 => address) public getApproved;

    event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);

    constructor(address initialOwner) {
        owner = initialOwner;
    }

    function safeMint(address to, uint256 tokenId) external {
        require(to != address(0), "zero recipient");
        require(ownerOf[tokenId] == address(0), "already minted");
        ownerOf[tokenId] = to;
        balanceOf[to]++;
        emit Transfer(address(0), to, tokenId);
    }

    function approve(address spender, uint256 tokenId) external {
        require(ownerOf[tokenId] == msg.sender, "not owner");
        getApproved[tokenId] = spender;
    }

    function transferFrom(address from, address to, uint256 tokenId) external {
        require(ownerOf[tokenId] == from, "wrong owner");
        require(msg.sender == from || getApproved[tokenId] == msg.sender, "not approved");
        _transfer(from, to, tokenId);
    }

    function roundTripTransfer(address to, uint256 tokenId) external {
        address from = ownerOf[tokenId];
        require(from == msg.sender, "not owner");
        require(to != from, "same recipient");
        _transfer(from, to, tokenId);
        _transfer(to, from, tokenId);
    }

    function _transfer(address from, address to, uint256 tokenId) private {
        require(to != address(0), "zero recipient");
        delete getApproved[tokenId];
        ownerOf[tokenId] = to;
        balanceOf[from]--;
        balanceOf[to]++;
        emit Transfer(from, to, tokenId);
    }
}
