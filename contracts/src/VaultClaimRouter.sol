// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/Address.sol";

/// @title VaultClaimRouter
/// @notice Routes bridged asset claims while enforcing a fixed royalty split.
contract VaultClaimRouter is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;
    using Address for address payable;

    uint256 public constant ROYALTY_BPS = 430;
    uint256 private constant BPS_DENOMINATOR = 10_000;

    address public royaltyReceiver;

    event ClaimRouted(
        bytes32 indexed claimId,
        address indexed vault,
        address indexed claimant,
        address asset,
        uint256 grossAmount,
        uint256 royaltyAmount,
        uint256 netAmount
    );

    event RoyaltyReceiverUpdated(address indexed previousReceiver, address indexed newReceiver);
    event RouterFunded(address indexed sender, uint256 amount);

    constructor(address initialRoyaltyReceiver) Ownable(msg.sender) {
        require(initialRoyaltyReceiver != address(0), "royalty receiver required");
        royaltyReceiver = initialRoyaltyReceiver;
    }

    /// @notice Updates the royalty collector address.
    function setRoyaltyReceiver(address newRoyaltyReceiver) external onlyOwner {
        require(newRoyaltyReceiver != address(0), "royalty receiver required");
        address previous = royaltyReceiver;
        royaltyReceiver = newRoyaltyReceiver;
        emit RoyaltyReceiverUpdated(previous, newRoyaltyReceiver);
    }

    /// @notice Calculates the royalty and net payout for a given gross amount.
    function previewRoyalty(uint256 amount) public pure returns (uint256 royaltyAmount, uint256 netAmount) {
        royaltyAmount = (amount * ROYALTY_BPS) / BPS_DENOMINATOR;
        netAmount = amount - royaltyAmount;
    }

    /// @notice Routes an ERC20 claim to the claimant and royalty receiver.
    /// @param claimId Identifier emitted for off-chain traceability.
    /// @param token ERC20 token being claimed.
    /// @param claimant Recipient of the net claim amount.
    /// @param amount Total amount claimed (gross before royalty).
    function routeERC20Claim(bytes32 claimId, IERC20 token, address claimant, uint256 amount)
        external
        nonReentrant
    {
        require(claimant != address(0), "invalid claimant");
        require(amount > 0, "amount required");
        address receiver = royaltyReceiver;
        require(receiver != address(0), "royalty receiver required");

        (uint256 royaltyAmount, uint256 netAmount) = previewRoyalty(amount);

        token.safeTransferFrom(msg.sender, receiver, royaltyAmount);
        token.safeTransferFrom(msg.sender, claimant, netAmount);

        emit ClaimRouted(claimId, msg.sender, claimant, address(token), amount, royaltyAmount, netAmount);
    }

    /// @notice Routes a native asset claim, forwarding the royalty split and remaining amount.
    /// @param claimId Identifier emitted for off-chain traceability.
    /// @param claimant Recipient of the net claim amount.
    function routeNativeClaim(bytes32 claimId, address payable claimant) external payable nonReentrant {
        require(claimant != address(0), "invalid claimant");
        require(msg.value > 0, "amount required");
        address payable receiver = payable(royaltyReceiver);
        require(receiver != address(0), "royalty receiver required");

        (uint256 royaltyAmount, uint256 netAmount) = previewRoyalty(msg.value);

        receiver.sendValue(royaltyAmount);
        claimant.sendValue(netAmount);

        emit ClaimRouted(claimId, msg.sender, claimant, address(0), msg.value, royaltyAmount, netAmount);
    }

    receive() external payable {
        emit RouterFunded(msg.sender, msg.value);
    }
}
