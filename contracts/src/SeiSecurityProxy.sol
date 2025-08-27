// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title SeiSecurityProxy
/// @notice Minimal stateless proxy exposing hooks for security modules.
/// @dev Implements role gating, proof decoding, memo interpretation and
/// recovery guard callbacks as described by the Advanced Security Proxy
/// Architecture.
contract SeiSecurityProxy {
    address public roleGate;
    address public proofDecoder;
    address public memoInterpreter;
    address public recoveryGuard;

    event RoleGateUpdated(address indexed newGate);
    event ProofDecoderUpdated(address indexed newDecoder);
    event MemoInterpreterUpdated(address indexed newInterpreter);
    event RecoveryGuardUpdated(address indexed newGuard);

    modifier onlyRole(bytes32 role, address account) {
        require(IRoleGate(roleGate).checkRole(role, account), "role denied");
        _;
    }

    function setRoleGate(address gate) external {
        roleGate = gate;
        emit RoleGateUpdated(gate);
    }

    function setProofDecoder(address decoder) external {
        proofDecoder = decoder;
        emit ProofDecoderUpdated(decoder);
    }

    function setMemoInterpreter(address interpreter) external {
        memoInterpreter = interpreter;
        emit MemoInterpreterUpdated(interpreter);
    }

    function setRecoveryGuard(address guard) external {
        recoveryGuard = guard;
        emit RecoveryGuardUpdated(guard);
    }

    function execute(
        bytes32 role,
        bytes calldata proof,
        bytes calldata memo,
        address target,
        bytes calldata data
    ) external onlyRole(role, msg.sender) returns (bytes memory) {
        require(IProofDecoder(proofDecoder).decode(proof, msg.sender), "invalid proof");
        IMemoInterpreter(memoInterpreter).interpret(memo, msg.sender, target);
        IRecoveryGuard(recoveryGuard).beforeCall(msg.sender, target, data);
        (bool ok, bytes memory res) = target.call(data);
        if (!ok) {
            IRecoveryGuard(recoveryGuard).handleFailure(msg.sender, target, data);
            revert("call failed");
        }
        IRecoveryGuard(recoveryGuard).afterCall(msg.sender, target, data, res);
        return res;
    }
}

interface IRoleGate {
    function checkRole(bytes32 role, address account) external view returns (bool);
}

interface IProofDecoder {
    function decode(bytes calldata proof, address account) external view returns (bool);
}

interface IMemoInterpreter {
    function interpret(bytes calldata memo, address account, address target) external;
}

interface IRecoveryGuard {
    function beforeCall(address account, address target, bytes calldata data) external;
    function handleFailure(address account, address target, bytes calldata data) external;
    function afterCall(address account, address target, bytes calldata data, bytes calldata result) external;
}
