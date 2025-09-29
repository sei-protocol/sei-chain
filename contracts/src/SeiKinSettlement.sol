// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.24;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/// @title SeiKinSettlement - Immutable Royalty Settlement Contract
/// @notice Enforces an 8.5% Kin royalty on verified cross-chain settlements (CCTP & CCIP)
contract SeiKinSettlement {
    using SafeERC20 for IERC20;

    // Constants
    uint256 public constant ROYALTY_BPS = 850;
    uint256 public constant BPS_DENOMINATOR = 10_000;

    // Immutable configuration
    address public immutable KIN_ROYALTY_VAULT;
    address public immutable TRUSTED_CCIP_ROUTER;
    address public immutable TRUSTED_CCTP_VERIFIER;

    // Replay protection
    mapping(bytes32 => bool) public settledDeposits;

    // Reentrancy guard
    uint256 private _status;
    uint256 private constant _NOT_ENTERED = 1;
    uint256 private constant _ENTERED = 2;

    // Events
    event RoyaltyPaid(address indexed payer, address indexed token, uint256 amount);
    event SettlementExecuted(address indexed to, address indexed token, uint256 netAmount);
    event DepositSettled(bytes32 indexed depositId, address indexed token, uint256 grossAmount);

    // Errors
    error UntrustedSender();
    error InvalidInstruction();
    error AlreadySettled();
    error TransferFailed();
    error ReentrancyBlocked();

    constructor(
        address royaltyVault_,
        address ccipRouter_,
        address cctpVerifier_
    ) {
        require(royaltyVault_ != address(0), "Zero royaltyVault");
        require(ccipRouter_ != address(0), "Zero CCIP router");
        require(cctpVerifier_ != address(0), "Zero CCTP verifier");

        KIN_ROYALTY_VAULT = royaltyVault_;
        TRUSTED_CCIP_ROUTER = ccipRouter_;
        TRUSTED_CCTP_VERIFIER = cctpVerifier_;
        _status = _NOT_ENTERED;
    }

    modifier onlyTrusted(address sender) {
        if (sender != TRUSTED_CCIP_ROUTER && sender != TRUSTED_CCTP_VERIFIER) {
            revert UntrustedSender();
        }
        _;
    }

    modifier nonReentrant() {
        if (_status == _ENTERED) revert ReentrancyBlocked();
        _status = _ENTERED;
        _;
        _status = _NOT_ENTERED;
    }

    /// @notice Royalty calculation from gross amount
    function royaltyInfo(uint256 gross) public pure returns (uint256 royalty, uint256 net) {
        royalty = (gross * ROYALTY_BPS) / BPS_DENOMINATOR;
        net = gross - royalty;
    }

    /// @notice Called by CCTP verifier to settle a deposit
    function settleFromCCTP(
        bytes32 depositId,
        address token,
        address destination,
        uint256 amount
    ) external onlyTrusted(msg.sender) nonReentrant {
        if (settledDeposits[depositId]) revert AlreadySettled();
        if (token == address(0) || destination == address(0) || amount == 0) revert InvalidInstruction();

        settledDeposits[depositId] = true;

        IERC20 tokenContract = IERC20(token);
        (uint256 royaltyAmt, uint256 netAmt) = royaltyInfo(amount);

        tokenContract.safeTransfer(KIN_ROYALTY_VAULT, royaltyAmt);
        emit RoyaltyPaid(destination, token, royaltyAmt);

        tokenContract.safeTransfer(destination, netAmt);
        emit SettlementExecuted(destination, token, netAmt);
        emit DepositSettled(depositId, token, amount);
    }

    /// @notice Called by Chainlink CCIP to trigger a payout
    function settleFromCCIP(
        address token,
        address destination,
        uint256 amount
    ) external onlyTrusted(msg.sender) nonReentrant {
        if (token == address(0) || destination == address(0) || amount == 0) revert InvalidInstruction();

        IERC20 tokenContract = IERC20(token);
        (uint256 royaltyAmt, uint256 netAmt) = royaltyInfo(amount);

        tokenContract.safeTransfer(KIN_ROYALTY_VAULT, royaltyAmt);
        emit RoyaltyPaid(destination, token, royaltyAmt);

        tokenContract.safeTransfer(destination, netAmt);
        emit SettlementExecuted(destination, token, netAmt);
    }

    /// @notice View function to check balance
    function balanceOf(address token) external view returns (uint256) {
        return IERC20(token).balanceOf(address(this));
    }
}
