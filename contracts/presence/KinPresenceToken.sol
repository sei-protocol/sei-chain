// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/extensions/ERC721URIStorage.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract KinPresenceToken is ERC721URIStorage, Ownable {
    uint256 public nextId;

    constructor() ERC721("KinPresence", "SOULSIGIL") {}

    function mintPresence(address to, string memory metadataURI) external onlyOwner {
        _mint(to, nextId);
        _setTokenURI(nextId, metadataURI);
        unchecked {
            nextId++;
        }
    }
}
