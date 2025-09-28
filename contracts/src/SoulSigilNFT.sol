// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC721/extensions/ERC721URIStorage.sol";

/// @title SoulSigilNFT
/// @notice Minimal ERC721 used to notarise KinBridge claims with off-chain proofs.
contract SoulSigilNFT is ERC721URIStorage, Ownable {

    uint256 private _nextTokenId;
    string private _baseTokenURI;

    mapping(bytes32 => bool) private _consumedProofs;
    mapping(uint256 => bytes32) private _tokenProofs;

    event SigilMinted(address indexed to, uint256 indexed tokenId, bytes32 indexed proofHash, string proof);
    event BaseURIUpdated(string previousBaseURI, string newBaseURI);

    constructor(string memory baseTokenURI_) Ownable(msg.sender) ERC721("Soul Sigil", "SIGIL") {
        _baseTokenURI = baseTokenURI_;
    }

    /// @notice Returns whether a proof hash has already been used for a mint.
    function hasConsumedProof(bytes32 proofHash) external view returns (bool) {
        return _consumedProofs[proofHash];
    }

    /// @notice Returns the stored proof hash for a token.
    function proofOf(uint256 tokenId) external view returns (bytes32) {
        require(_exists(tokenId), "query for nonexistent token");
        return _tokenProofs[tokenId];
    }

    /// @notice Updates the base URI that is prefixed to every token URI.
    function setBaseURI(string calldata newBaseURI) external onlyOwner {
        string memory previous = _baseTokenURI;
        _baseTokenURI = newBaseURI;
        emit BaseURIUpdated(previous, newBaseURI);
    }

    /// @notice Mints a new sigil for the specified account with the provided proof string.
    /// @dev Proof strings are hashed to enforce uniqueness while keeping the raw string in the event log.
    function mint(address to, string calldata proof) external onlyOwner returns (uint256) {
        require(to != address(0), "invalid recipient");
        require(bytes(proof).length != 0, "proof required");

        bytes32 proofHash = keccak256(bytes(proof));
        require(!_consumedProofs[proofHash], "proof already used");

        uint256 tokenId = ++_nextTokenId;

        _safeMint(to, tokenId);
        _tokenProofs[tokenId] = proofHash;
        _consumedProofs[proofHash] = true;

        if (bytes(_baseTokenURI).length != 0) {
            string memory uri = string.concat(_baseTokenURI, _toHexString(proofHash));
            _setTokenURI(tokenId, uri);
        }

        emit SigilMinted(to, tokenId, proofHash, proof);
        return tokenId;
    }

    function _toHexString(bytes32 value) private pure returns (string memory) {
        bytes memory alphabet = "0123456789abcdef";
        bytes memory str = new bytes(64);
        for (uint256 i = 0; i < 32; i++) {
            str[i * 2] = alphabet[uint8(value[i] >> 4)];
            str[i * 2 + 1] = alphabet[uint8(value[i] & 0x0f)];
        }
        return string.concat("0x", string(str));
    }

    function _burn(uint256 tokenId) internal override(ERC721, ERC721URIStorage) {
        super._burn(tokenId);
    }

    function tokenURI(uint256 tokenId) public view override(ERC721, ERC721URIStorage) returns (string memory) {
        return super.tokenURI(tokenId);
    }
}
