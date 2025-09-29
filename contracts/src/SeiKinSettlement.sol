```solidity
// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.24;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {CCIPReceiver} from "./ccip/CCIPReceiver.sol";
import {Client} from "./ccip/Client.sol";

/// @title SeiKinSettlement Protocol
/// @notice Enforces an immutable 8.5% Kin royalty on settlements received via Circle CCTP or Chainlink CCIP.
contract SeiKinSettlement is CCIPReceiver, ReentrancyGuard {
    using SafeERC20 for IERC20;

    uint256 public constant ROYALTY_BPS = 850;
    uint256 public constant BPS_DENOMINATOR = 10_000;

    address public immutable KIN_ROYALTY_VAULT;
    address public immutable TRUSTED_CCIP_SENDER;
    address public immutable TRUSTED_CCTP_SENDER;

    mapping(bytes32 => bool) public settledDeposits;

    event RoyaltyPaid(address indexed payer, uint256 royaltyAmount);
    event SettlementTransferred(address indexed to, uint256 amountAfterRoyalty);
    event CCIPReceived(address indexed sender, string message);
    event CCTPReceived(address indexed sender, string message);
    event DepositSettled(bytes32 indexed depositId, address indexed token, uint256 grossAmount);

    error UntrustedSender();
    error AlreadySettled();
    error InvalidInstruction();

    constructor(
        address router,
        address kinRoyaltyVault,
        address trustedCcipSender,
        address trustedCctpSender
    ) CCIPReceiver(router) {
        require(kinRoyaltyVault != address(0), "Zero address");
        require(trustedCcipSender != address(0), "Zero address");
        require(trustedCctpSender != address(0), "Zero address");

        KIN_ROYALTY_VAULT = kinRoyaltyVault;
        TRUSTED_CCIP_SENDER = trustedCcipSender;
        TRUSTED_CCTP_SENDER = trustedCctpSender;
    }

    modifier onlyTrusted(address sender) {
        if (sender != TRUSTED_CCIP_SENDER && sender != TRUSTED_CCTP_SENDER) {
            revert UntrustedSender();
        }
        _;
    }

    function royaltyInfo(uint256 amount) public pure returns (uint256 royaltyAmount, uint256 netAmount) {
        if (amount == 0) return (0, 0);
        royaltyAmount = (amount * ROYALTY_BPS) / BPS_DENOMINATOR;
        netAmount = amount - royaltyAmount;
    }

    function settleFromCCTP(
        bytes32 depositId,
        address token,
        address destination,
        uint256 amount,
        bytes calldata message
    ) external nonReentrant onlyTrusted(msg.sender) {
        if (settledDeposits[depositId]) revert AlreadySettled();
        if (token == address(0) || destination == address(0) || amount == 0) revert InvalidInstruction();

        settledDeposits[depositId] = true;

        IERC20 settlementToken = IERC20(token);
        (uint256 royaltyAmount, uint256 netAmount) = royaltyInfo(amount);

        settlementToken.safeTransfer(KIN_ROYALTY_VAULT, royaltyAmount);
        emit RoyaltyPaid(destination, royaltyAmount);

        settlementToken.safeTransfer(destination, netAmount);
        emit SettlementTransferred(destination, netAmount);
        emit CCTPReceived(destination, _bytesToString(message));
        emit DepositSettled(depositId, token, amount);
    }

    function _ccipReceive(Client.Any2EVMMessage memory message)
        internal
        override
        nonReentrant
    {
        address decodedSender = abi.decode(message.sender, (address));
        if (decodedSender != TRUSTED_CCIP_SENDER) revert UntrustedSender();

        address token = abi.decode(message.data, (address));
        require(token != address(0), "Zero address");

        IERC20 settlementToken = IERC20(token);
        uint256 amount = settlementToken.balanceOf(address(this));
        require(amount > 0, "Zero amount");

        address payer = tx.origin;
        (uint256 royaltyAmount, uint256 netAmount) = royaltyInfo(amount);

        settlementToken.safeTransfer(KIN_ROYALTY_VAULT, royaltyAmount);
        emit RoyaltyPaid(payer, royaltyAmount);

        settlementToken.safeTransfer(payer, netAmount);
        emit SettlementTransferred(payer, netAmount);
        emit CCIPReceived(decodedSender, "Settlement via CCIP");
    }

    function balanceOf(address token) external view returns (uint256) {
        return IERC20(token).balanceOf(address(this));
    }

    function _bytesToString(bytes memory data) private pure returns (string memory) {
        return data.length == 0 ? "" : string(data);
    }
}
```
