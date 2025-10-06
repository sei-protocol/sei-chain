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
/// @notice Routes validated SeiMesh presence proofs to payouts and SoulSigil mints.
contract VaultClaimRouter is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    struct VaultConfig {
        IERC20 asset;
        uint256 payoutAmount;
        bool active;
        string defaultTokenURI;
    }

    mapping(bytes32 => VaultConfig) public vaults;
    mapping(bytes32 => mapping(bytes32 => bool)) public proofRegistry;
    mapping(bytes32 => mapping(bytes32 => bool)) public proofConsumed;

    IKinPresenceToken public presenceToken;

    event VaultConfigured(bytes32 indexed vaultId, address asset, uint256 payoutAmount, string defaultTokenURI);
    event VaultStatusUpdated(bytes32 indexed vaultId, bool active);
    event ProofStatus(bytes32 indexed vaultId, bytes32 indexed proofHash, bool allowed);
    event PresenceTokenUpdated(address indexed token);
    event ProofClaimed(bytes32 indexed vaultId, bytes32 indexed proofHash, address indexed beneficiary, string metadataURI);

    constructor() Ownable(msg.sender) {}

    function setPresenceToken(address token) external onlyOwner {
        presenceToken = IKinPresenceToken(token);
        emit PresenceTokenUpdated(token);
    }

    function configureVault(
        bytes32 vaultId,
        IERC20 asset,
        uint256 payoutAmount,
        string calldata defaultTokenURI,
        bool active
    ) external onlyOwner {
        vaults[vaultId] = VaultConfig({
            asset: asset,
            payoutAmount: payoutAmount,
            active: active,
            defaultTokenURI: defaultTokenURI
        });
        emit VaultConfigured(vaultId, address(asset), payoutAmount, defaultTokenURI);
        emit VaultStatusUpdated(vaultId, active);
    }

    function setVaultStatus(bytes32 vaultId, bool active) external onlyOwner {
        VaultConfig storage vault = vaults[vaultId];
        require(address(vault.asset) != address(0) || vault.payoutAmount == 0, "vault_uninitialized");
        vault.active = active;
        emit VaultStatusUpdated(vaultId, active);
    }

    function allowProof(bytes32 vaultId, bytes32 proofHash, bool allowed) external onlyOwner {
        proofRegistry[vaultId][proofHash] = allowed;
        emit ProofStatus(vaultId, proofHash, allowed);
    }

    function submitProof(
        bytes32 vaultId,
        bytes32 proofHash,
        address beneficiary,
        string calldata metadataURI
    ) external nonReentrant {
        VaultConfig storage vault = vaults[vaultId];
        require(vault.active, "vault_inactive");
        require(proofRegistry[vaultId][proofHash], "proof_not_authorized");
        require(!proofConsumed[vaultId][proofHash], "proof_already_used");
        require(beneficiary != address(0), "invalid_beneficiary");

        proofConsumed[vaultId][proofHash] = true;

        if (address(vault.asset) != address(0) && vault.payoutAmount > 0) {
            vault.asset.safeTransfer(beneficiary, vault.payoutAmount);
        }

        if (address(presenceToken) != address(0)) {
            string memory tokenURI = bytes(metadataURI).length > 0 ? metadataURI : vault.defaultTokenURI;
            presenceToken.mintPresence(beneficiary, tokenURI);
        }

        emit ProofClaimed(vaultId, proofHash, beneficiary, metadataURI);
    }
}
