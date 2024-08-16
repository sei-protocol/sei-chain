// SPDX-License-Identifier: MIT 
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract MockERC721 is ERC721, Ownable {
    uint256 private _currentTokenId = 0;

    constructor(string memory name, string memory symbol) ERC721(name, symbol) Ownable(msg.sender) {}

    function mint(address to) public onlyOwner {
        _currentTokenId++;
        _mint(to, _currentTokenId);
    }

    function batchMint(address to, uint256 amount) public onlyOwner {
        for (uint256 i = 0; i < amount; i++) {
            _currentTokenId++;
            _mint(to, _currentTokenId);
        }
    }

    function burn(uint256 tokenId) public {
        _burn(tokenId);
    }
}