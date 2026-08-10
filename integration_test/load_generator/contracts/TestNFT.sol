// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

contract TestNFT {
    string public constant name = "ReplayNFT";
    string public constant symbol = "RNFT";
    address public immutable owner;
    mapping(uint256 => address) public ownerOf;
    mapping(address => uint256) public balanceOf;

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
}
