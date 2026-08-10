// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * Replays a bounded call-tree and operation profile against one allowlisted
 * helper. It reproduces pressure/topology, not source state or return values.
 * CREATE/CREATE2 are represented by bounded hashing rather than deployments.
 */
contract CallGraphHarness {
    uint256 public constant MAX_FRAMES = 64;
    uint256 public constant MAX_DEPTH = 8;
    uint256 private entered;
    CallGraphNode public immutable node;

    error MalformedSpec();
    error Reentrant();

    constructor() {
        node = new CallGraphNode(address(this));
    }

    function execute(bytes calldata spec, uint256 salt) external payable {
        if (entered != 0) revert Reentrant();
        uint256 frames = spec.length / 16;
        if (spec.length == 0 || spec.length % 16 != 0 || frames > MAX_FRAMES) {
            revert MalformedSpec();
        }
        uint8 previousDepth;
        for (uint256 i; i < frames; i++) {
            uint8 depth = uint8(spec[i * 16 + 1]);
            uint8 callType = uint8(spec[i * 16]);
            if (
                depth > MAX_DEPTH ||
                callType > 4 ||
                (i == 0 && depth != 0) ||
                (i != 0 && depth > previousDepth + 1)
            ) revert MalformedSpec();
            previousDepth = depth;
        }
        entered = 1;
        node.perform(spec, 0, frames, salt);
        entered = 0;
    }
}

contract CallGraphNode {
    uint256 private constant MAX_OPS_PER_KIND = 16;
    uint256 private constant MAX_GAS_BURN = 100_000;
    address private immutable HARNESS;
    address private immutable SELF;

    event SimulatedLog(bytes32 indexed digest, uint256 index);

    error Unauthorized();
    error MalformedFrame();
    error ChildCallFailed();

    struct Controls {
        uint256 reads;
        uint256 writes;
        uint256 logs;
        uint256 hashes;
        uint256 gasBurn;
        uint256 creates;
    }

    constructor(address harness) {
        HARNESS = harness;
        SELF = address(this);
    }

    function perform(
        bytes calldata spec,
        uint256 index,
        uint256 end,
        uint256 salt
    ) external returns (bytes32 digest) {
        if (msg.sender != HARNESS && msg.sender != SELF) revert Unauthorized();
        if (index >= end || end > spec.length / 16) revert MalformedFrame();
        uint8 depth = uint8(spec[index * 16 + 1]);
        digest = _work(spec, index, salt);
        _children(spec, index, end, salt, depth);
    }

    function _work(
        bytes calldata spec,
        uint256 index,
        uint256 salt
    ) private returns (bytes32 digest) {
        Controls memory controls = _controls(spec, index * 16);
        if (
            controls.reads > MAX_OPS_PER_KIND ||
            controls.writes > MAX_OPS_PER_KIND ||
            controls.logs > MAX_OPS_PER_KIND ||
            controls.hashes > MAX_OPS_PER_KIND ||
            controls.creates > MAX_OPS_PER_KIND ||
            controls.gasBurn > MAX_GAS_BURN
        ) revert MalformedFrame();

        digest = keccak256(abi.encodePacked(salt, index, msg.sender));
        for (uint256 i; i < controls.reads; i++) {
            bytes32 key = keccak256(abi.encodePacked("read", salt, index, i));
            assembly {
                digest := xor(digest, sload(key))
            }
        }
        for (uint256 i; i < controls.writes; i++) {
            bytes32 key = keccak256(abi.encodePacked("write", salt, index, i));
            bytes32 value = keccak256(abi.encodePacked(digest, i));
            assembly {
                sstore(key, value)
            }
        }
        for (uint256 i; i < controls.hashes + controls.creates; i++) {
            digest = keccak256(abi.encodePacked(digest, salt, i));
        }
        for (uint256 i; i < controls.logs; i++) emit SimulatedLog(digest, i);

        uint256 startGas = gasleft();
        while (startGas - gasleft() < controls.gasBurn && gasleft() > 100_000) {
            digest = keccak256(abi.encodePacked(digest));
        }
    }

    function _children(
        bytes calldata spec,
        uint256 index,
        uint256 end,
        uint256 salt,
        uint8 depth
    ) private {
        uint256 cursor = index + 1;
        while (cursor < end) {
            uint8 childDepth = uint8(spec[cursor * 16 + 1]);
            if (childDepth != depth + 1) revert MalformedFrame();
            uint256 childEnd = cursor + 1;
            while (childEnd < end && uint8(spec[childEnd * 16 + 1]) > childDepth) childEnd++;
            _dispatch(uint8(spec[cursor * 16]), spec, cursor, childEnd, salt);
            cursor = childEnd;
        }
    }

    function _controls(bytes calldata spec, uint256 offset) private pure returns (Controls memory value) {
        value.reads = _u16(spec, offset + 2);
        value.writes = _u16(spec, offset + 4);
        value.logs = _u16(spec, offset + 6);
        value.hashes = _u16(spec, offset + 8);
        value.gasBurn = _u32(spec, offset + 10);
        value.creates = _u16(spec, offset + 14);
    }

    function _dispatch(
        uint8 callType,
        bytes calldata spec,
        uint256 start,
        uint256 end,
        uint256 salt
    ) private {
        bytes memory payload = abi.encodeCall(this.perform, (spec, start, end, salt));
        bool success;
        if (callType == 1) {
            (success, ) = SELF.staticcall(payload);
        } else if (callType == 2) {
            (success, ) = SELF.delegatecall(payload);
        } else {
            // CREATE and CREATE2 frames use a normal allowlisted call; their
            // deployment pressure is represented by the frame's create count.
            (success, ) = SELF.call(payload);
        }
        if (!success) revert ChildCallFailed();
    }

    function _u16(bytes calldata data, uint256 offset) private pure returns (uint256) {
        return (uint256(uint8(data[offset])) << 8) | uint256(uint8(data[offset + 1]));
    }

    function _u32(bytes calldata data, uint256 offset) private pure returns (uint256) {
        return
            (uint256(uint8(data[offset])) << 24) |
            (uint256(uint8(data[offset + 1])) << 16) |
            (uint256(uint8(data[offset + 2])) << 8) |
            uint256(uint8(data[offset + 3]));
    }
}
