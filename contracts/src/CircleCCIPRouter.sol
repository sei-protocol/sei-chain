// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {SeiKinSettlement} from "./SeiKinSettlement.sol";

/// @title CircleCCIPRouter
/// @notice Consumes CCIP messages, performs routing validation and forwards
/// settlement instructions to the SeiKin settlement contract.
contract CircleCCIPRouter {
    /// @notice Administrative account able to update configuration.
    address public owner;

    /// @notice Settlement contract that enforces royalties and proof checks.
    SeiKinSettlement public settlement;

    /// @notice External verifier validating CCIP message authenticity.
    ICCIPMessageVerifier public ccipVerifier;

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event SettlementUpdated(address indexed newSettlement);
    event CcipVerifierUpdated(address indexed newVerifier);
    event TransferRouted(
        bytes32 indexed depositId,
        address indexed token,
        address indexed destination,
        uint256 grossAmount,
        uint256 royaltyAmount
    );

    error NotOwner();
    error InvalidAddress();
    error InvalidMessage();
    error VerificationFailed();
    error MisconfiguredSettlement();
    error NoChange();

    struct RoutedTransfer {
        bytes32 depositId;
        address token;
        address destination;
        uint256 amount;
    }

    constructor(address settlement_, address ccipVerifier_) {
        if (settlement_ == address(0) || ccipVerifier_ == address(0)) {
            revert InvalidAddress();
        }
        owner = msg.sender;
        settlement = SeiKinSettlement(settlement_);
        ccipVerifier = ICCIPMessageVerifier(ccipVerifier_);
        emit OwnershipTransferred(address(0), msg.sender);
        emit SettlementUpdated(settlement_);
        emit CcipVerifierUpdated(ccipVerifier_);
    }

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    /// @notice Updates the CCIP verifier contract.
    function setCcipVerifier(address newVerifier) external onlyOwner {
        if (newVerifier == address(0)) revert InvalidAddress();
        if (address(ccipVerifier) == newVerifier) revert NoChange();
        ccipVerifier = ICCIPMessageVerifier(newVerifier);
        emit CcipVerifierUpdated(newVerifier);
    }

    /// @notice Points the router at a new settlement contract.
    function setSettlement(address newSettlement) external onlyOwner {
        if (newSettlement == address(0)) revert InvalidAddress();
        if (address(settlement) == newSettlement) revert NoChange();
        settlement = SeiKinSettlement(newSettlement);
        emit SettlementUpdated(newSettlement);
    }

    /// @notice Transfers contract ownership.
    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert InvalidAddress();
        address previous = owner;
        if (previous == newOwner) revert NoChange();
        owner = newOwner;
        emit OwnershipTransferred(previous, newOwner);
    }

    /// @notice Decodes a CCIP payload into the routed transfer format.
    function decodeMessage(bytes calldata message) public pure returns (RoutedTransfer memory decoded) {
        (
            bytes32 depositId,
            address token,
            address destination,
            uint256 amount
        ) = abi.decode(message, (bytes32, address, address, uint256));
        decoded = RoutedTransfer({depositId: depositId, token: token, destination: destination, amount: amount});
    }

    /// @notice Computes the split applied to a gross amount.
    function previewSplit(uint256 amount) external view returns (uint256 netAmount, uint256 royaltyAmount) {
        royaltyAmount = settlement.previewRoyalty(amount);
        netAmount = settlement.previewNetAmount(amount);
    }

    /// @notice Verifies proofs, decodes the CCIP payload and forwards settlement instructions.
    /// @param message Raw CCIP message payload containing routing information.
    /// @param proof External verification payload for the CCIP message.
    /// @param cctpProof Proof used by the settlement contract to validate the Circle mint.
    function route(bytes calldata message, bytes calldata proof, bytes calldata cctpProof)
        external
        returns (uint256 netAmount, uint256 royaltyAmount)
    {
        if (!ccipVerifier.verify(message, proof)) revert VerificationFailed();

        if (settlement.router() != address(this)) revert MisconfiguredSettlement();

        RoutedTransfer memory decoded = decodeMessage(message);
        if (decoded.destination == address(0) || decoded.token == address(0)) revert InvalidMessage();
        if (decoded.amount == 0) revert InvalidMessage();

        royaltyAmount = settlement.previewRoyalty(decoded.amount);
        uint256 expectedNetAmount = settlement.previewNetAmount(decoded.amount);

        SeiKinSettlement.SettlementInstruction memory instruction = SeiKinSettlement.SettlementInstruction({
            depositId: decoded.depositId,
            token: decoded.token,
            destination: decoded.destination,
            amount: decoded.amount,
            royaltyAmount: royaltyAmount
        });

        netAmount = settlement.settle(instruction, cctpProof);
        if (netAmount != expectedNetAmount) revert MisconfiguredSettlement();

        emit TransferRouted(decoded.depositId, decoded.token, decoded.destination, decoded.amount, royaltyAmount);
    }
}

interface ICCIPMessageVerifier {
    function verify(bytes calldata message, bytes calldata proof) external view returns (bool);
}
