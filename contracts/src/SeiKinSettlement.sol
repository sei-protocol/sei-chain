// SPDX-License-Identifier: MIT
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

    uint256 private constant ROYALTY_BPS = 850;
    uint256 private constant BPS_DENOMINATOR = 10_000;

    address public immutable KIN_ROYALTY_VAULT;
    address public immutable TRUSTED_CCIP_SENDER;
    address public immutable TRUSTED_CCTP_SENDER;

    event RoyaltyPaid(address indexed payer, uint256 royaltyAmount);
    event SettlementTransferred(address indexed to, uint256 amountAfterRoyalty);
    event CCIPReceived(address indexed sender, string message);
    event CCTPReceived(address indexed sender, string message);

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
        require(
            sender == TRUSTED_CCIP_SENDER || sender == TRUSTED_CCTP_SENDER,
            "Untrusted sender"
        );
        _;
    }

    /// @notice Returns the royalty amount and net amount for a provided gross amount.
    function royaltyInfo(uint256 amount) public pure returns (uint256 royaltyAmount, uint256 netAmount) {
        if (amount == 0) {
            return (0, 0);
        }
        royaltyAmount = (amount * ROYALTY_BPS) / BPS_DENOMINATOR;
        netAmount = amount - royaltyAmount;
    }

    /// @notice Circle CCTP callback entrypoint. Tokens must already be transferred to this contract.
    function onCCTPReceived(
        address token,
        address from,
        uint256 amount,
        bytes calldata message
    ) external nonReentrant onlyTrusted(msg.sender) {
        require(token != address(0), "Zero address");
        require(from != address(0), "Zero address");
        require(amount > 0, "Zero amount");

        IERC20 settlementToken = IERC20(token);
        uint256 royaltyAmount = _collectRoyalty(settlementToken, amount, from);
        uint256 netAmount = amount - royaltyAmount;

        settlementToken.safeTransfer(from, netAmount);
        emit SettlementTransferred(from, netAmount);
        emit CCTPReceived(from, _bytesToString(message));
    }

    /// @inheritdoc CCIPReceiver
    function _ccipReceive(Client.Any2EVMMessage memory message)
        internal
        override
        nonReentrant
    {
        address decodedSender = abi.decode(message.sender, (address));
        _requireTrusted(decodedSender);

        address token = abi.decode(message.data, (address));
        require(token != address(0), "Zero address");

        IERC20 settlementToken = IERC20(token);
        uint256 amount = settlementToken.balanceOf(address(this));
        require(amount > 0, "Zero amount");

        address payer = tx.origin;
        uint256 royaltyAmount = _collectRoyalty(settlementToken, amount, payer);
        uint256 netAmount = amount - royaltyAmount;

        settlementToken.safeTransfer(payer, netAmount);
        emit SettlementTransferred(payer, netAmount);
        emit CCIPReceived(decodedSender, "Settlement via CCIP");
    }

    function _collectRoyalty(IERC20 token, uint256 amount, address payer) private returns (uint256 royaltyAmount) {
        (royaltyAmount, ) = royaltyInfo(amount);
        if (royaltyAmount > 0) {
            token.safeTransfer(KIN_ROYALTY_VAULT, royaltyAmount);
            emit RoyaltyPaid(payer, royaltyAmount);
        }
    }

    function _bytesToString(bytes memory data) private pure returns (string memory) {
        if (data.length == 0) {
            return "";
        }
        return string(data);
    }

    function _requireTrusted(address sender) private view {
        require(
            sender == TRUSTED_CCIP_SENDER || sender == TRUSTED_CCTP_SENDER,
            "Untrusted sender"
        );
    }
}
