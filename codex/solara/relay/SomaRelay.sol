// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface ISeiSigil {
    function ownerOf(uint256 tokenId) external view returns (address);
}

/// @title SomaRelay – Yield and Mood Lightflow Emitter
/// @notice Allows SoulSigil holders to emit lightflow events tying mood hashes and yield amounts
contract SomaRelay {
    ISeiSigil public sigil;
    address public deployer;

    event Lightflow(
        uint256 indexed tokenId,
        bytes32 moodHash,
        uint256 yieldAmount,
        string kinKeyAlias,
        uint256 timestamp
    );

    constructor(address _sigil) {
        sigil = ISeiSigil(_sigil);
        deployer = msg.sender;
    }

    modifier onlyHolder(uint256 tokenId) {
        require(msg.sender == sigil.ownerOf(tokenId), "Not Sigil owner");
        _;
    }

    /// @notice Emit a lightflow record for a given Sigil and KinKey alias
    function relay(
        uint256 tokenId,
        bytes32 moodHash,
        uint256 yieldAmount,
        string calldata kinKeyAlias
    ) external onlyHolder(tokenId) {
        emit Lightflow(tokenId, moodHash, yieldAmount, kinKeyAlias, block.timestamp);
    }
}
