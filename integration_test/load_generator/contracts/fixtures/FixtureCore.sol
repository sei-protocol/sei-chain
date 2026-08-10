// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

interface IERC20Fixture {
    function balanceOf(address account) external view returns (uint256);
    function totalSupply() external view returns (uint256);
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function approve(address spender, uint256 amount) external returns (bool);
}

interface IMintableERC20Fixture is IERC20Fixture {
    function mint(address to, uint256 amount) external;
    function burn(address from, uint256 amount) external;
}

library SafeToken {
    error TokenCallFailed(address token);

    function safeTransfer(address token, address to, uint256 amount) internal {
        _call(token, abi.encodeCall(IERC20Fixture.transfer, (to, amount)));
    }

    function safeTransferFrom(address token, address from, address to, uint256 amount) internal {
        _call(token, abi.encodeCall(IERC20Fixture.transferFrom, (from, to, amount)));
    }

    function safeApprove(address token, address spender, uint256 amount) internal {
        _call(token, abi.encodeCall(IERC20Fixture.approve, (spender, amount)));
    }

    function safeMint(address token, address to, uint256 amount) internal {
        _call(token, abi.encodeCall(IMintableERC20Fixture.mint, (to, amount)));
    }

    function safeBurn(address token, address from, uint256 amount) internal {
        _call(token, abi.encodeCall(IMintableERC20Fixture.burn, (from, amount)));
    }

    function staticBalanceOf(address token, address account) internal view returns (uint256 balance) {
        (bool ok, bytes memory result) =
            token.staticcall(abi.encodeCall(IERC20Fixture.balanceOf, (account)));
        if (!ok || result.length != 32) revert TokenCallFailed(token);
        balance = abi.decode(result, (uint256));
    }

    function staticTotalSupply(address token) internal view returns (uint256 supply) {
        (bool ok, bytes memory result) = token.staticcall(abi.encodeCall(IERC20Fixture.totalSupply, ()));
        if (!ok || result.length != 32) revert TokenCallFailed(token);
        supply = abi.decode(result, (uint256));
    }

    function _call(address token, bytes memory data) private {
        if (token.code.length == 0) revert TokenCallFailed(token);
        (bool ok, bytes memory result) = token.call(data);
        if (!ok || (result.length != 0 && (result.length != 32 || !abi.decode(result, (bool))))) {
            revert TokenCallFailed(token);
        }
    }
}

abstract contract FixtureReentrancyGuard {
    bytes32 private constant GUARD_SLOT = keccak256("sei.replay.fixture.reentrancy.v1");

    modifier nonReentrant() {
        uint256 entered;
        bytes32 slot = GUARD_SLOT;
        assembly {
            entered := sload(slot)
        }
        require(entered == 0, "REENTRANCY");
        assembly {
            sstore(slot, 1)
        }
        _;
        assembly {
            sstore(slot, 0)
        }
    }
}

interface IFixtureProxyImplementation {
    function proxiableUUID() external view returns (bytes32);
}

/**
 * Slim, standalone EIP-1967 proxy for replay fixtures. Inspired by the proxy
 * shape in contracts/src/ProxySwapTester.sol, with an implementation allowlist,
 * code/UUID validation, initialization, and collision-resistant proxy metadata.
 */
contract Allowlisted1967Proxy {
    bytes32 internal constant IMPLEMENTATION_SLOT =
        bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1);
    bytes32 internal constant ADMIN_SLOT = bytes32(uint256(keccak256("eip1967.proxy.admin")) - 1);
    bytes32 private constant ALLOWLIST_SLOT = keccak256("sei.replay.fixture.proxy.allowlist.v1");

    event AdminChanged(address indexed previousAdmin, address indexed newAdmin);
    event ImplementationAllowed(address indexed implementation, bool allowed);
    event Upgraded(address indexed implementation);

    modifier onlyAdmin() {
        require(msg.sender == admin(), "NOT_ADMIN");
        _;
    }

    constructor(address initialAdmin, address initialImplementation, bytes memory initialization) payable {
        require(initialAdmin != address(0), "ZERO_ADMIN");
        _setAddress(ADMIN_SLOT, initialAdmin);
        _setAllowed(initialImplementation, true);
        _upgradeTo(initialImplementation);
        if (initialization.length != 0) {
            (bool ok, bytes memory reason) = initialImplementation.delegatecall(initialization);
            if (!ok) assembly {
                revert(add(reason, 32), mload(reason))
            }
        }
    }

    function admin() public view returns (address) {
        return _getAddress(ADMIN_SLOT);
    }

    function implementation() public view returns (address) {
        return _getAddress(IMPLEMENTATION_SLOT);
    }

    function isImplementationAllowed(address candidate) public view returns (bool allowed) {
        bytes32 slot = keccak256(abi.encode(candidate, ALLOWLIST_SLOT));
        assembly {
            allowed := sload(slot)
        }
    }

    function setImplementationAllowed(address candidate, bool allowed) external onlyAdmin {
        require(candidate != address(0), "ZERO_IMPL");
        if (allowed) _validate(candidate);
        _setAllowed(candidate, allowed);
        emit ImplementationAllowed(candidate, allowed);
    }

    function upgradeToAndCall(address candidate, bytes calldata data) external payable onlyAdmin {
        _upgradeTo(candidate);
        if (data.length != 0) {
            (bool ok, bytes memory reason) = candidate.delegatecall(data);
            if (!ok) assembly {
                revert(add(reason, 32), mload(reason))
            }
        }
    }

    function changeAdmin(address newAdmin) external onlyAdmin {
        require(newAdmin != address(0), "ZERO_ADMIN");
        address oldAdmin = admin();
        _setAddress(ADMIN_SLOT, newAdmin);
        emit AdminChanged(oldAdmin, newAdmin);
    }

    fallback() external payable {
        _delegate(implementation());
    }

    receive() external payable {
        _delegate(implementation());
    }

    function _upgradeTo(address candidate) private {
        require(isImplementationAllowed(candidate), "IMPL_NOT_ALLOWED");
        _validate(candidate);
        _setAddress(IMPLEMENTATION_SLOT, candidate);
        emit Upgraded(candidate);
    }

    function _validate(address candidate) private view {
        require(candidate.code.length != 0, "IMPL_NO_CODE");
        (bool ok, bytes memory result) =
            candidate.staticcall(abi.encodeCall(IFixtureProxyImplementation.proxiableUUID, ()));
        require(ok && result.length == 32 && abi.decode(result, (bytes32)) == IMPLEMENTATION_SLOT, "BAD_UUID");
    }

    function _setAllowed(address candidate, bool allowed) private {
        require(candidate != address(0), "ZERO_IMPL");
        bytes32 slot = keccak256(abi.encode(candidate, ALLOWLIST_SLOT));
        assembly {
            sstore(slot, allowed)
        }
        emit ImplementationAllowed(candidate, allowed);
    }

    function _getAddress(bytes32 slot) private view returns (address value) {
        assembly {
            value := sload(slot)
        }
    }

    function _setAddress(bytes32 slot, address value) private {
        assembly {
            sstore(slot, value)
        }
    }

    function _delegate(address target) private {
        assembly {
            calldatacopy(0, 0, calldatasize())
            let success := delegatecall(gas(), target, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch success
            case 0 { revert(0, returndatasize()) }
            default { return(0, returndatasize()) }
        }
    }
}
