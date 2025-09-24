// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {CCIPReceiver} from "./ccip/CCIPReceiver.sol";
import {Client} from "./ccip/Client.sol";

/// @title SeiKinSettlement
/// @notice Settlement contract enforcing an immutable 8.5% Kin royalty on every bridged transfer.
/// @dev The contract is compatible with both Circle's CCTP callbacks and Chainlink CCIP deliveries.
///      Trusted senders act as sovereign keepers that cannot be updated once the contract is deployed,
///      ensuring there is no upgrade or governance backdoor.
contract SeiKinSettlement is CCIPReceiver, ReentrancyGuard {
    using SafeERC20 for IERC20;

    /// @dev Basis points denominator (100%).
    uint256 private constant BPS_DENOMINATOR = 10_000;

    /// @dev Kin royalty share expressed in basis points (8.5%).
    uint256 private constant ROYALTY_BPS = 850;

    /// @notice Receiver of the Kin royalty share for every settlement.
    address public immutable kinRoyaltyVault;

    /// @notice Trusted CCIP sender on the source chain. Encoded as an EVM address.
    address public immutable trustedCcipSender;

    /// @notice Trusted Circle CCTP caller on this chain.
    address public immutable trustedCctpCaller;

    /// @notice Registry of sovereign keepers recognised by the contract.
    mapping(address => bool) private _keepers;

    address[] private _keeperList;

    /// @notice Raised when an address parameter is the zero address.
    error ZeroAddress();

    /// @notice Raised when a provided amount is zero.
    error ZeroAmount();

    /// @notice Raised when attempting to settle with insufficient escrowed funds.
    error InsufficientBalance(address token, uint256 expected, uint256 actual);

    /// @notice Raised when a CCIP message originates from an unexpected sender.
    error UntrustedCcipSender(address sender);

    /// @notice Raised when CCTP tries to invoke the contract from an untrusted caller.
    error UntrustedCctpCaller(address caller);

    /// @notice Raised when decoding settlement instructions fails.
    error InvalidSettlementInstruction();

    event RoyaltyPaid(address indexed payer, uint256 royaltyAmount);
    event SettlementTransferred(address indexed to, uint256 amountAfterRoyalty);
    event CCIPReceived(address indexed sender, string message);
    event CCTPReceived(address indexed sender, string message);
    event KeeperRegistered(address indexed keeper);

    struct SettlementInstruction {
        address beneficiary;
        bytes metadata;
    }

    constructor(
        address router,
        address royaltyVault,
        address ccipSender,
        address cctpCaller
    ) CCIPReceiver(router) {
        if (royaltyVault == address(0) || ccipSender == address(0) || cctpCaller == address(0)) {
            revert ZeroAddress();
        }

        kinRoyaltyVault = royaltyVault;
        trustedCcipSender = ccipSender;
        trustedCctpCaller = cctpCaller;

        _registerKeeper(ccipSender);
        _registerKeeper(cctpCaller);
    }

    /// @notice Returns the list of sovereign keepers recognised by the protocol.
    function keeperList() external view returns (address[] memory) {
        return _keeperList;
    }

    /// @notice Checks if an address is a registered keeper.
    function isKeeper(address account) external view returns (bool) {
        return _keepers[account];
    }

    /// @notice Preview royalty breakdown for an arbitrary amount.
    function royaltyInfo(uint256 amount) public pure returns (uint256 royaltyAmount, uint256 netAmount) {
        if (amount == 0) {
            return (0, 0);
        }
        royaltyAmount = (amount * ROYALTY_BPS) / BPS_DENOMINATOR;
        netAmount = amount - royaltyAmount;
    }

    /// @notice Circle CCTP entry point. The trusted CCTP contract should mint or transfer the
    ///         specified {amount} of {token} to this contract before invoking the callback.
    function onCCTPReceived(
        address token,
        address from,
        uint256 amount,
        bytes calldata message
    ) external nonReentrant {
        if (msg.sender != trustedCctpCaller) {
            revert UntrustedCctpCaller(msg.sender);
        }
        if (token == address(0) || from == address(0)) {
            revert ZeroAddress();
        }
        if (amount == 0) {
            revert ZeroAmount();
        }

        _ensureBalance(token, amount);

        uint256 royaltyAmount = _collectRoyalty(token, from, amount);
        uint256 netAmount = amount - royaltyAmount;

        IERC20(token).safeTransfer(from, netAmount);
        emit SettlementTransferred(from, netAmount);
        emit CCTPReceived(from, _bytesToString(message));
    }

    /// @inheritdoc CCIPReceiver
    function _ccipReceive(Client.Any2EVMMessage memory message) internal override nonReentrant {
        address decodedSender = _decodeSender(message.sender);
        if (decodedSender != trustedCcipSender) {
            revert UntrustedCcipSender(decodedSender);
        }

        SettlementInstruction memory instruction = _decodeInstruction(message.data);
        if (instruction.beneficiary == address(0)) {
            revert InvalidSettlementInstruction();
        }

        uint256 tokenCount = message.destTokenAmounts.length;
        if (tokenCount == 0) {
            revert InvalidSettlementInstruction();
        }

        for (uint256 i = 0; i < tokenCount; i++) {
            Client.EVMTokenAmount memory tokenAmount = message.destTokenAmounts[i];
            if (tokenAmount.token == address(0)) {
                revert InvalidSettlementInstruction();
            }
            if (tokenAmount.amount == 0) {
                revert ZeroAmount();
            }

            _ensureBalance(tokenAmount.token, tokenAmount.amount);

            uint256 royaltyAmount = _collectRoyalty(tokenAmount.token, instruction.beneficiary, tokenAmount.amount);
            uint256 netAmount = tokenAmount.amount - royaltyAmount;

            IERC20(tokenAmount.token).safeTransfer(instruction.beneficiary, netAmount);
            emit SettlementTransferred(instruction.beneficiary, netAmount);
        }

        emit CCIPReceived(decodedSender, _bytesToString(instruction.metadata));
    }

    /// @dev Collects royalties and emits a payment event.
    function _collectRoyalty(address token, address payer, uint256 amount) private returns (uint256 royaltyAmount) {
        (royaltyAmount, ) = royaltyInfo(amount);
        if (royaltyAmount > 0) {
            IERC20(token).safeTransfer(kinRoyaltyVault, royaltyAmount);
            emit RoyaltyPaid(payer, royaltyAmount);
        }
    }

    function _decodeInstruction(bytes memory data) private pure returns (SettlementInstruction memory instruction) {
        if (data.length == 0) {
            return instruction;
        }

        if (data.length == 32) {
            instruction.beneficiary = abi.decode(data, (address));
            return instruction;
        }

        if (data.length >= 64) {
            instruction = abi.decode(data, (SettlementInstruction));
            return instruction;
        }

        revert InvalidSettlementInstruction();
    }

    function _decodeSender(bytes memory data) private pure returns (address sender) {
        if (data.length != 32) {
            revert InvalidSettlementInstruction();
        }
        sender = abi.decode(data, (address));
    }

    function _ensureBalance(address token, uint256 amount) private view {
        uint256 balance = IERC20(token).balanceOf(address(this));
        if (balance < amount) {
            revert InsufficientBalance(token, amount, balance);
        }
    }

    function _bytesToString(bytes memory data) private pure returns (string memory) {
        if (data.length == 0) {
            return "";
        }
        return string(data);
    }

    function _registerKeeper(address keeper) private {
        if (keeper == address(0)) {
            revert ZeroAddress();
        }
        if (_keepers[keeper]) {
            return;
        }
        _keepers[keeper] = true;
        _keeperList.push(keeper);
        emit KeeperRegistered(keeper);
    }
}
