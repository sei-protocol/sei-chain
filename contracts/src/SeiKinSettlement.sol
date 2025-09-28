// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title SeiKinSettlement
/// @notice Coordinates Circle CCTP mint proofs with CCIP settlement
/// instructions while enforcing a fixed royalty distribution.
contract SeiKinSettlement {
    /// @dev Basis point denominator used for royalty math.
    uint16 public constant BPS_DENOMINATOR = 10_000;

    /// @dev Royalty share expressed in basis points (8.5%).
    uint16 public constant ROYALTY_BPS = 850;

    /// @notice Address controlling admin level configuration.
    address public owner;

    /// @notice Router authorized to feed CCIP settlement instructions.
    address public router;

    /// @notice Account receiving the royalty cut of every settlement.
    address public royaltyRecipient;

    /// @notice External verifier responsible for validating CCTP mints.
    ICctpVerifier public cctpVerifier;

    /// @notice Tracks processed deposits to prevent double settlement.
    mapping(bytes32 => bool) public settledDeposits;

    /// @notice Reentrancy status flags.
    uint256 private constant _STATUS_NOT_ENTERED = 1;
    uint256 private constant _STATUS_ENTERED = 2;
    uint256 private _status;

    /// @notice Settlement payload produced by the CCIP router.
    struct SettlementInstruction {
        bytes32 depositId;
        address token;
        address destination;
        uint256 amount;
        uint256 royaltyAmount;
    }

    // === Events ===
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event RouterUpdated(address indexed previousRouter, address indexed newRouter);
    event RoyaltyRecipientUpdated(address indexed previousRecipient, address indexed newRecipient);
    event CctpVerifierUpdated(address indexed previousVerifier, address indexed newVerifier);
    event SettlementFinalized(
        bytes32 indexed depositId,
        address indexed token,
        address indexed destination,
        uint256 grossAmount,
        uint256 royaltyAmount
    );

    // === Errors ===
    error NotOwner();
    error NotRouter();
    error InvalidAddress();
    error InvalidAmount();
    error InvalidInstruction();
    error SettlementReplay();
    error VerificationModuleMissing();
    error TransferFailed();
    error InsufficientFunds();
    error NoChange();
    error ReentrancyBlocked();

    constructor(address royaltyRecipient_, address cctpVerifier_) {
        if (royaltyRecipient_ == address(0) || cctpVerifier_ == address(0)) {
            revert InvalidAddress();
        }

        owner = msg.sender;
        royaltyRecipient = royaltyRecipient_;
        cctpVerifier = ICctpVerifier(cctpVerifier_);
        _status = _STATUS_NOT_ENTERED;

        emit OwnershipTransferred(address(0), msg.sender);
        emit RoyaltyRecipientUpdated(address(0), royaltyRecipient_);
        emit CctpVerifierUpdated(address(0), cctpVerifier_);
    }

    // === Modifiers ===
    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    modifier onlyRouter() {
        if (msg.sender != router) revert NotRouter();
        _;
    }

    modifier nonReentrant() {
        if (_status == _STATUS_ENTERED) revert ReentrancyBlocked();
        _status = _STATUS_ENTERED;
        _;
        _status = _STATUS_NOT_ENTERED;
    }

    // === Admin Functions ===

    function setRouter(address newRouter) external onlyOwner {
        if (newRouter == address(0)) revert InvalidAddress();
        if (router == newRouter) revert NoChange();

        address previous = router;
        router = newRouter;
        emit RouterUpdated(previous, newRouter);
    }

    function updateRoyaltyRecipient(address newRecipient) external onlyOwner {
        if (newRecipient == address(0)) revert InvalidAddress();
        if (royaltyRecipient == newRecipient) revert NoChange();

        address previous = royaltyRecipient;
        royaltyRecipient = newRecipient;
        emit RoyaltyRecipientUpdated(previous, newRecipient);
    }

    function updateCctpVerifier(address newVerifier) external onlyOwner {
        if (newVerifier == address(0)) revert InvalidAddress();
        if (address(cctpVerifier) == newVerifier) revert NoChange();

        address previous = address(cctpVerifier);
        cctpVerifier = ICctpVerifier(newVerifier);
        emit CctpVerifierUpdated(previous, newVerifier);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert InvalidAddress();
        if (owner == newOwner) revert NoChange();

        address previous = owner;
        owner = newOwner;
        emit OwnershipTransferred(previous, newOwner);
    }

    // === Royalty Math ===

    function previewRoyalty(uint256 amount) public pure returns (uint256 royaltyAmount) {
        royaltyAmount = (amount * ROYALTY_BPS) / BPS_DENOMINATOR;
    }

    function previewNetAmount(uint256 amount) public pure returns (uint256 netAmount) {
        uint256 royaltyAmount = previewRoyalty(amount);
        if (amount < royaltyAmount) revert InvalidAmount();
        netAmount = amount - royaltyAmount;
    }

    // === Settlement ===

    function settle(SettlementInstruction calldata instruction, bytes calldata cctpProof)
        external
        onlyRouter
        nonReentrant
        returns (uint256 netAmount)
    {
        // Basic validations
        if (
            instruction.destination == address(0) ||
            instruction.token == address(0) ||
            instruction.amount == 0
        ) revert InvalidInstruction();

        if (settledDeposits[instruction.depositId]) revert SettlementReplay();
        if (address(cctpVerifier) == address(0)) revert VerificationModuleMissing();

        // Royalty enforcement
        uint256 expectedRoyalty = previewRoyalty(instruction.amount);
        if (instruction.royaltyAmount != expectedRoyalty) revert InvalidInstruction();

        // Mint proof validation
        (
            bytes32 depositId,
            address proofToken,
            uint256 proofAmount,
            address mintRecipient
        ) = cctpVerifier.validateMint(cctpProof);

        if (
            depositId != instruction.depositId ||
            proofToken != instruction.token ||
            proofAmount != instruction.amount ||
            mintRecipient != address(this)
        ) revert InvalidInstruction();

        uint256 balance = IERC20(instruction.token).balanceOf(address(this));
        if (balance < instruction.amount) revert InsufficientFunds();

        settledDeposits[instruction.depositId] = true;

        uint256 royaltyAmount = instruction.royaltyAmount;
        netAmount = instruction.amount - royaltyAmount;

        if (!_transferToken(instruction.token, royaltyRecipient, royaltyAmount)) revert TransferFailed();
        if (!_transferToken(instruction.token, instruction.destination, netAmount)) revert TransferFailed();

        emit SettlementFinalized(
            instruction.depositId,
            instruction.token,
            instruction.destination,
            instruction.amount,
            royaltyAmount
        );
    }

    // === Read ===

    function balanceOf(address token) external view returns (uint256) {
        return IERC20(token).balanceOf(address(this));
    }

    // === Internals ===

    function _transferToken(address token, address to, uint256 value) private returns (bool success) {
        if (value == 0) return true;
        (success, bytes memory data) = token.call(
            abi.encodeWithSelector(IERC20.transfer.selector, to, value)
        );
        if (!success) return false;
        if (data.length == 0) return true;
        return abi.decode(data, (bool));
    }
}

// === Interfaces ===

interface IERC20 {
    function transfer(address to, uint256 value) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

interface ICctpVerifier {
    function validateMint(bytes calldata proof)
        external
        view
        returns (
            bytes32 depositId,
            address token,
            uint256 amount,
            address mintRecipient
        );
}
