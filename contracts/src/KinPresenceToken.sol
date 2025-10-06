// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/extensions/ERC721URIStorage.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

/// @title KinPresenceToken
/// @notice SoulSigil token minted after a verified SeiMesh presence proof.
/// @dev Combines the original verifier-gated claim model with ERC721 SoulSigil minting.
contract KinPresenceToken is ERC721URIStorage, Ownable {
    /// @notice Address allowed to authorize mints (e.g., an oracle/verifier or router contract).
    address public verifier;

    /// @notice Tracks addresses that have already claimed a SoulSigil.
    mapping(address => bool) public hasClaimed;

    /// @notice The next tokenId to mint.
    uint256 public nextId;

    /// @notice Emitted when a presence SoulSigil is minted for a user.
    event PresenceMinted(address indexed to, uint256 indexed tokenId, bytes32 wifiHash, string tokenURI);

    constructor(address _verifier) ERC721("KinPresence", "SOULSIGIL") Ownable(msg.sender) {
        verifier = _verifier;
    }

    /// @notice Allows the owner to update the verifier address.
    function setVerifier(address _verifier) external onlyOwner {
        verifier = _verifier;
    }

    /// @notice Claim a SoulSigil presence token after a verified SeiMesh presence proof.
    /// @dev Restricted to the configured verifier. Each address can claim only once.
    function claim(address to, bytes32 wifiHash, string memory metadataURI) external {
        require(msg.sender == verifier, "Only verifier allowed");
        require(!hasClaimed[to], "Already claimed");

        hasClaimed[to] = true;

        uint256 tokenId = nextId;
        _safeMint(to, tokenId);
        _setTokenURI(tokenId, metadataURI);

        emit PresenceMinted(to, tokenId, wifiHash, metadataURI);

        unchecked {
            nextId = tokenId + 1;
        }
    }
}
