// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract KinKeyPresenceValidator {
    address public sovereign;
    mapping(address => bytes32) public latestMoodHash;
    mapping(address => bytes32) private latestEntropyValue;

    constructor() {
        sovereign = msg.sender;
    }

    function submitPresence(
        address user,
        string calldata mood,
        uint256 timestamp,
        bytes32 entropy,
        bytes calldata signature
    ) external {
        require(block.timestamp - timestamp < 10, "Stale presence");
        bytes32 structHash = keccak256(
            abi.encode(
                keccak256("Presence(address user,string mood,uint256 timestamp,bytes32 entropy)"),
                user,
                keccak256(bytes(mood)),
                timestamp,
                entropy
            )
        );
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", structHash));
        address recovered = recoverSigner(digest, signature);
        require(recovered == user, "Invalid signature");
        latestEntropyValue[user] = entropy;
        latestMoodHash[user] = keccak256(abi.encodePacked(mood, entropy));
    }

    function isPresent(address user, string memory mood) public view returns (bool) {
        bytes32 entropy = latestEntropy(user);
        if (entropy == bytes32(0) && latestMoodHash[user] == bytes32(0)) {
            return false;
        }
        return latestMoodHash[user] == keccak256(abi.encodePacked(mood, entropy));
    }

    function latestEntropy(address user) internal view returns (bytes32) {
        return latestEntropyValue[user];
    }

    function recoverSigner(bytes32 digest, bytes memory sig) internal pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSig(sig);
        return ecrecover(digest, v, r, s);
    }

    function splitSig(bytes memory sig) internal pure returns (bytes32 r, bytes32 s, uint8 v) {
        require(sig.length == 65, "Invalid signature length");
        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }
    }
}
