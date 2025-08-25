// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface ISeiSigil {
    function ownerOf(uint256 tokenId) external view returns (address);
}

contract KinKeyVault {
    ISeiSigil public sigil;
    address public deployer;

    struct KeySlot {
        string label;
        bytes32 pubkeyHash;
        uint256 lastRotated;
    }

    mapping(uint256 => KeySlot[]) public keysByToken;
    mapping(uint256 => uint256) public activeKeyIndex;

    event KeyAdded(uint256 indexed tokenId, string label, bytes32 pubkeyHash);
    event KeyRotated(uint256 indexed tokenId, uint256 newIndex);

    constructor(address _sigil) {
        sigil = ISeiSigil(_sigil);
        deployer = msg.sender;
    }

    modifier onlyHolder(uint256 tokenId) {
        require(msg.sender == sigil.ownerOf(tokenId), "Not Sigil owner");
        _;
    }

    function addKey(
        uint256 tokenId,
        string calldata label,
        bytes32 pubkeyHash
    ) external onlyHolder(tokenId) {
        keysByToken[tokenId].push(KeySlot({
            label: label,
            pubkeyHash: pubkeyHash,
            lastRotated: block.timestamp
        }));
        emit KeyAdded(tokenId, label, pubkeyHash);
    }

    function rotateKey(uint256 tokenId, uint256 newIndex) external onlyHolder(tokenId) {
        require(newIndex < keysByToken[tokenId].length, "Invalid index");
        activeKeyIndex[tokenId] = newIndex;
        keysByToken[tokenId][newIndex].lastRotated = block.timestamp;
        emit KeyRotated(tokenId, newIndex);
    }

    function getActiveKey(uint256 tokenId) external view returns (KeySlot memory) {
        return keysByToken[tokenId][activeKeyIndex[tokenId]];
    }

    function getKeyCount(uint256 tokenId) external view returns (uint256) {
        return keysByToken[tokenId].length;
    }
}
