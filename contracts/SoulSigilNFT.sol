// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

/// @title SoulSigilNFT - soulbound sigil registry used by the Seivault Kinmodule.
/// @notice Lightweight ERC721-inspired registry where tokens cannot be transferred.
contract SoulSigilNFT {
    event SigilMinted(address indexed to, uint256 indexed tokenId, bytes32 sigilHash);
    event SigilHeartbeat(address indexed owner, bytes32 latestProof);

    string public constant name = "SoulSigil";
    string public constant symbol = "SIGIL";

    address public immutable curator;

    mapping(uint256 => address) private _owners;
    mapping(address => uint256) private _balances;
    mapping(address => bytes32) private _sigilHash;
    mapping(address => bytes32) private _livePresenceProof;

    constructor(address curator_) {
        require(curator_ != address(0), "curator required");
        curator = curator_;
    }

    modifier onlyCurator() {
        require(msg.sender == curator, "only curator");
        _;
    }

    function balanceOf(address owner) external view returns (uint256) {
        require(owner != address(0), "zero owner");
        return _balances[owner];
    }

    function ownerOf(uint256 tokenId) external view returns (address) {
        address owner = _owners[tokenId];
        require(owner != address(0), "soul missing");
        return owner;
    }

    function getSigilHash(address owner) external view returns (bytes32) {
        return _sigilHash[owner];
    }

    /// @notice Mint a new soul sigil. Existing holders cannot mint a second sigil.
    function mint(address to, uint256 tokenId, bytes32 sigilHash) external onlyCurator {
        require(to != address(0), "zero to");
        require(_owners[tokenId] == address(0), "token exists");
        require(_sigilHash[to] == bytes32(0), "sigil bound");

        _owners[tokenId] = to;
        _balances[to] += 1;
        _sigilHash[to] = sigilHash;

        emit SigilMinted(to, tokenId, sigilHash);
    }

    /// @notice Update liveness proof for the owner.
    function attestLiveness(bytes32 latestProof) external {
        require(_sigilHash[msg.sender] != bytes32(0), "no sigil");
        _livePresenceProof[msg.sender] = latestProof;
        emit SigilHeartbeat(msg.sender, latestProof);
    }

    function livePresenceProof(address owner) external view returns (bytes32) {
        return _livePresenceProof[owner];
    }
}
