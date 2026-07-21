// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

/**
 * Test fixture: forwards arbitrary calldata to a target through a real CALL,
 * STATICCALL, or DELEGATECALL so specs can exercise precompile dispatch
 * semantics from deployed contract bytecode (interpreter dispatch, readOnly
 * and delegatecall guards, value forwarding) instead of only via direct
 * EOA-to-precompile transactions.
 *
 * On failure the target's revert data is bubbled up unchanged so specs can
 * assert the precompile's own error, not this contract's.
 */
contract PrecompileCaller {
    function callTarget(address target, bytes calldata data)
        external
        payable
        returns (bytes memory)
    {
        (bool ok, bytes memory ret) = target.call{value: msg.value}(data);
        if (!ok) _bubble(ret);
        return ret;
    }

    function staticcallTarget(address target, bytes calldata data)
        external
        view
        returns (bytes memory)
    {
        (bool ok, bytes memory ret) = target.staticcall(data);
        if (!ok) _bubble(ret);
        return ret;
    }

    function delegatecallTarget(address target, bytes calldata data)
        external
        returns (bytes memory)
    {
        (bool ok, bytes memory ret) = target.delegatecall(data);
        if (!ok) _bubble(ret);
        return ret;
    }

    function _bubble(bytes memory ret) private pure {
        if (ret.length == 0) revert("PrecompileCaller: call failed");
        assembly {
            revert(add(ret, 0x20), mload(ret))
        }
    }
}
