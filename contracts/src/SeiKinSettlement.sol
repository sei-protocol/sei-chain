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

    /// @notice Settlement payload produced by the CCIP router.
    struct SettlementInstruction {
        bytes32 depositId;
        address token;
        address destination;
        uint256 amount;
        uint256 royaltyAmount;
    }

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

    uint256 private constant _STATUS_NOT_ENTERED = 1;
    uint256 private constant _STATUS_ENTERED = 2;
    uint256 private _status;

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

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    modifier onlyRouter() {
        if (msg.sender != router) revert NotRouter();
        _;
    }

    /// @notice Assigns a router allowed to finalize settlements.
    /// @param newRouter Address of the Circle CCIP router implementation.
    function setRouter(address newRouter) external onlyOwner {
        if (newRouter == address(0)) revert InvalidAddress();
        address previous = router;
        if (previous == newRouter) revert NoChange();
        router = newRouter;
        emit RouterUpdated(previous, newRouter);
    }

    /// @notice Updates the address receiving royalty payouts.
    /// @param newRecipient Address collecting the enforced royalties.
    function updateRoyaltyRecipient(address newRecipient) external onlyOwner {
        if (newRecipient == address(0)) revert InvalidAddress();
        address previous = royaltyRecipient;
        if (previous == newRecipient) revert NoChange();
        royaltyRecipient = newRecipient;
        emit RoyaltyRecipientUpdated(previous, newRecipient);
    }

    /// @notice Updates the verifier used to validate CCTP mint proofs.
    /// @param newVerifier Address of the verifier contract.
    function updateCctpVerifier(address newVerifier) external onlyOwner {
        if (newVerifier == address(0)) revert InvalidAddress();
        address previous = address(cctpVerifier);
        if (previous == newVerifier) revert NoChange();
        cctpVerifier = ICctpVerifier(newVerifier);
        emit CctpVerifierUpdated(previous, newVerifier);
    }

    /// @notice Transfers ownership to a new administrator.
    /// @param newOwner Address receiving control permissions.
    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert InvalidAddress();
        address previous = owner;
        if (previous == newOwner) revert NoChange();
        owner = newOwner;
        emit OwnershipTransferred(previous, newOwner);
    }

    /// @notice Computes the royalty that must be withheld for the provided amount.
    /// @param amount Gross settlement amount.
    /// @return royaltyAmount Portion of `amount` earmarked for royalties.
    function previewRoyalty(uint256 amount) public pure returns (uint256 royaltyAmount) {
        royaltyAmount = (amount * ROYALTY_BPS) / BPS_DENOMINATOR;
    }

    /// @notice Computes the beneficiary share after royalties are deducted.
    /// @param amount Gross settlement amount.
    /// @return netAmount Payout sent to the CCIP destination.
    function previewNetAmount(uint256 amount) public pure returns (uint256 netAmount) {
        uint256 royaltyAmount = previewRoyalty(amount);
        if (amount < royaltyAmount) revert InvalidAmount();
        netAmount = amount - royaltyAmount;
    }

    modifier nonReentrant() {
        if (_status == _STATUS_ENTERED) revert ReentrancyBlocked();
        _status = _STATUS_ENTERED;
        _;
        _status = _STATUS_NOT_ENTERED;
    }

    /// @notice Finalizes a settlement after both CCTP and CCIP proofs are validated.
    /// @param instruction Settlement breakdown generated by the CCIP router.
    /// @param cctpProof Attestation proving the Circle CCTP mint.
    /// @return netAmount Amount distributed to the CCIP destination.
    function settle(SettlementInstruction calldata instruction, bytes calldata cctpProof)
        external
        onlyRouter
        nonReentrant
        returns (uint256 netAmount)
    {
        if (instruction.destination == address(0) || instruction.token == address(0)) {
            revert InvalidInstruction();
        }
        if (instruction.amount == 0) revert InvalidAmount();
        if (settledDeposits[instruction.depositId]) revert SettlementReplay();
        if (address(cctpVerifier) == address(0)) revert VerificationModuleMissing();

        uint256 expectedRoyalty = previewRoyalty(instruction.amount);
        if (instruction.royaltyAmount != expectedRoyalty) revert InvalidInstruction();

        (
            bytes32 depositId,
            address proofToken,
            uint256 proofAmount,
            address mintRecipient
        ) = cctpVerifier.validateMint(cctpProof);

        if (depositId != instruction.depositId || proofToken != instruction.token) {
            revert InvalidInstruction();
        }
        if (proofAmount != instruction.amount || mintRecipient != address(this)) {
            revert InvalidInstruction();
        }

        uint256 balance = IERC20(instruction.token).balanceOf(address(this));
        if (balance < instruction.amount) revert InsufficientFunds();

        settledDeposits[instruction.depositId] = true;

        uint256 royaltyAmount = instruction.royaltyAmount;
        netAmount = instruction.amount - royaltyAmount;

        if (!_transferToken(instruction.token, royaltyRecipient, royaltyAmount)) {
            revert TransferFailed();
        }
        if (!_transferToken(instruction.token, instruction.destination, netAmount)) {
            revert TransferFailed();
        }

        emit SettlementFinalized(
            instruction.depositId,
            instruction.token,
            instruction.destination,
            instruction.amount,
            royaltyAmount
        );
    }

    /// @notice Returns the current ERC20 balance held by this contract.
    function balanceOf(address token) external view returns (uint256) {
        return IERC20(token).balanceOf(address(this));
    }

    function _transferToken(address token, address to, uint256 value) private returns (bool success) {
        if (value == 0) return true;
        (success, bytes memory data) = token.call(abi.encodeWithSelector(IERC20.transfer.selector, to, value));
        if (!success) return false;
        if (data.length == 0) return true;
        return abi.decode(data, (bool));
    }
}

interface IERC20 {
    function transfer(address to, uint256 value) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

interface ICctpVerifier {
    function validateMint(bytes calldata proof)
        external
        view
        returns (bytes32 depositId, address token, uint256 amount, address mintRecipient);
}
