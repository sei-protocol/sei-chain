// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./SeiSecurityProxy.sol";

/// @notice Simple mock implementations of proxy modules used in tests.
contract MockRoleGate is IRoleGate {
    bytes32 public constant DEFAULT_ROLE = keccak256("DEFAULT_ROLE");
    function checkRole(bytes32 role, address) external pure override returns (bool) {
        return role == DEFAULT_ROLE;
    }
}

contract MockProofDecoder is IProofDecoder {
    function decode(bytes calldata, address) external pure override returns (bool) {
        return true;
    }
}

contract MockMemoInterpreter is IMemoInterpreter {
    event Memo(address sender, bytes memo, address target);
    function interpret(bytes calldata memo, address sender, address target) external override {
        emit Memo(sender, memo, target);
    }
}

contract MockRecoveryGuard is IRecoveryGuard {
    event Before(address sender, address target);
    event After(address sender, address target);
    function beforeCall(address account, address target, bytes calldata) external override {
        emit Before(account, target);
    }
    function handleFailure(address, address, bytes calldata) external pure override {}
    function afterCall(address account, address target, bytes calldata, bytes calldata) external override {
        emit After(account, target);
    }
}
