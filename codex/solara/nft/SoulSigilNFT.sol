// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/extensions/ERC721URIStorage.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract SoulSigilNFT is ERC721URIStorage, Ownable {
    uint256 public nextTokenId = 1;
    mapping(address => bool) public hasSigil;

    constructor() ERC721("SoulSigil", "SIGIL") {}

    function mint(address to, string memory uri) external onlyOwner {
        require(!hasSigil[to], "Already owns Sigil");
        uint256 tokenId = nextTokenId++;
        _mint(to, tokenId);
        _setTokenURI(tokenId, uri);
        hasSigil[to] = true;
    }

    function burn(uint256 tokenId) external {
        require(ownerOf(tokenId) == msg.sender, "Not your Sigil");
        _burn(tokenId);
        hasSigil[msg.sender] = false;
    }

    function ownsSigil(address user) external view returns (bool) {
        return hasSigil[user];
    }
}
