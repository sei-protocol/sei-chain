// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

interface IKinPresenceToken {
    function mintPresence(address to, string memory metadataURI) external;
}

/// @title VaultClaimRouter
/// @notice Routes validated SeiMesh presence proofs to reward payouts and SoulSigil mints.
contract VaultClaimRouter is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    /// @notice Vault configuration for each proof domain (e.g., SSID or location hash).
    struct VaultConfig {
        IERC20 asset;              // Token to pay out
        uint256 payoutAmount;      // Amount per claim
        bool active;               // Is this vault accepting claims
        string defaultTokenURI;    // URI for SoulSigil metadata
        IKinPresenceToken soulSigil; // Reference to mintable SoulSigil contract
    }

    /// @notice vaultId => VaultConfig
    mapping(bytes32 => VaultConfig) public vaults;

    /// @notice vaultId => moodHash => wasClaimed
    mapping(bytes32 => mapping(bytes32 => bool)) public proofRegistry;

    /// @notice Emitted when a user claims a reward and receives their SoulSigil.
    event TokensClaimed(address indexed user, bytes32 indexed vaultId, uint256 amount, bytes32 moodHash);

    /// @notice Registers or updates a vault config.
    function configureVault(
        bytes32 vaultId,
        address asset,
        uint256 payoutAmount,
        string calldata defaultTokenURI,
        address soulSigil
    ) external onlyOwner {
        require(asset != address(0), "Invalid asset");
        require(soulSigil != address(0), "Invalid SoulSigil");

        vaults[vaultId] = VaultConfig({
            asset: IERC20(asset),
            payoutAmount: payoutAmount,
            active: true,
            defaultTokenURI: defaultTokenURI,
            soulSigil: IKinPresenceToken(soulSigil)
        });
    }

    /// @notice Disables a vault (no further claims allowed).
    function deactivateVault(bytes32 vaultId) external onlyOwner {
        vaults[vaultId].active = false;
    }

    /// @notice Claims a reward + NFT for a presence proof (one-time per moodHash per vault).
    /// @dev `vaultId` is typically the keccak of SSID or location, `moodHash` is a proof from a mood oracle.
    function claimPresenceReward(
        bytes32 vaultId,
        bytes32 moodHash,
        address to
    ) external nonReentrant {
        VaultConfig memory vault = vaults[vaultId];
        require(vault.active, "Vault inactive");
        require(!proofRegistry[vaultId][moodHash], "Already claimed");

        proofRegistry[vaultId][moodHash] = true;

        vault.asset.safeTransfer(to, vault.payoutAmount);
        vault.soulSigil.mintPresence(to, vault.defaultTokenURI);

        emit TokensClaimed(to, vaultId, vault.payoutAmount, moodHash);
    }
}
