// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/extensions/ERC721URIStorage.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

/// @title KinPresenceToken
/// @notice SoulSigil token minted after a verified SeiMesh presence proof.
contract KinPresenceToken is ERC721URIStorage, Ownable {
    uint256 public nextId;

    event PresenceMinted(address indexed to, uint256 indexed tokenId, string tokenURI);

    constructor() ERC721("KinPresence", "SOULSIGIL") Ownable(msg.sender) {}

    /// @notice Mint a new presence SoulSigil to `to`.
    /// @dev Restricted to the contract owner which is expected to be a
    ///      coordinating VaultClaimRouter or other controller contract.
    function mintPresence(address to, string memory metadataURI) external onlyOwner {
        uint256 tokenId = nextId;
        _safeMint(to, tokenId);
        _setTokenURI(tokenId, metadataURI);
        emit PresenceMinted(to, tokenId, metadataURI);
        unchecked {
            nextId = tokenId + 1;
        }
    }
}
