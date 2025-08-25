// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/extensions/ERC721URIStorage.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

interface IKinProof {
    function moodProofs(address) external view returns (bytes32);
    function royaltyRate() external view returns (uint256);
}

/// @title SeiSigilNFT — Mood-bound identity token for Sei
/// @notice Mints soul-bound NFTs tied to moodHash and exposes Kin royalty rate
contract SeiSigilNFT is ERC721URIStorage, Ownable {
    uint256 public nextTokenId;
    address public kinProof;
    mapping(address => bool) public hasMinted;

    event SigilMinted(address indexed user, uint256 tokenId, bytes32 moodHash);
    event KinProofUpdated(address kinProof);

    constructor() ERC721("SeiSigil", "SSIGIL") {}

    /// @notice Set the KinProof contract used for mood verification
    function setKinProof(address _kinProof) external onlyOwner {
        kinProof = _kinProof;
        emit KinProofUpdated(_kinProof);
    }

    /// @notice Mint a soul-bound Sigil if a mood proof exists
    function mint(string memory uri) external {
        require(!hasMinted[msg.sender], "Already minted");
        require(kinProof != address(0), "KinProof not set");

        bytes32 moodHash = IKinProof(kinProof).moodProofs(msg.sender);
        require(moodHash != bytes32(0), "No mood proof");

        uint256 tokenId = nextTokenId++;
        _safeMint(msg.sender, tokenId);
        _setTokenURI(tokenId, uri);
        hasMinted[msg.sender] = true;

        emit SigilMinted(msg.sender, tokenId, moodHash);
    }

    /// @dev Prevent transfers to keep tokens soul-bound
    function _beforeTokenTransfer(address from, address to, uint256 tokenId, uint256 batchSize)
        internal
        override
    {
        require(from == address(0) || to == address(0), "Soul bound");
        super._beforeTokenTransfer(from, to, tokenId, batchSize);
    }

    /// @notice Query the Kin royalty rate from the KinProof contract
    function getRoyaltyRate() external view returns (uint256) {
        require(kinProof != address(0), "KinProof not set");
        return IKinProof(kinProof).royaltyRate();
    }
}

