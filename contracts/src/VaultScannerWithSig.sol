// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";

/// @title VaultScannerWithSig
/// @notice Verifies withdrawal reports emitted by Kin vaults using an off-chain guardian signature.
contract VaultScannerWithSig {
    using ECDSA for bytes32;

    /// @notice Off-chain signer that attests to the authenticity of vault withdrawals.
    address public immutable guardianSigner;

    /// @notice Emitted whenever a withdrawal is scanned and validated.
    event WithdrawalScanned(
        address indexed user,
        address indexed token,
        uint256 amount,
        bytes32 indexed txHash,
        bool verified
    );

    /// @param _guardianSigner Address that produces guardian signatures. Cannot be zero.
    constructor(address _guardianSigner) {
        require(_guardianSigner != address(0), "Guardian signer zero");
        guardianSigner = _guardianSigner;
    }

    /// @notice Validates a withdrawal report signed by the guardian and emits an audit trail event.
    /// @param user The Kin account that performed the withdrawal.
    /// @param token The token contract that was withdrawn.
    /// @param amount Amount of tokens withdrawn.
    /// @param txHash Hash of the underlying settlement transaction used as a reference.
    /// @param guardianSig Guardian signature over the withdrawal payload.
    function scanAndVerify(
        address user,
        address token,
        uint256 amount,
        bytes32 txHash,
        bytes calldata guardianSig
    ) external {
        bytes32 digest = keccak256(abi.encode(user, token, amount, txHash));
        address recovered = MessageHashUtils.toEthSignedMessageHash(digest).recover(guardianSig);
        require(recovered == guardianSigner, "Guardian signature invalid");

        emit WithdrawalScanned(user, token, amount, txHash, true);
    }
}
