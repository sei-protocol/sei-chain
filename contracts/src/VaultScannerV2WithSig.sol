// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract VaultScannerV2WithSig {
    address public signer;
    mapping(bytes32 => bool) public scanned;

    event VaultScanned(address indexed user, uint256 amount, string purpose, bytes32 scanHash);

    constructor(address _signer) {
        signer = _signer;
    }

    function scan(
        address user,
        uint256 amount,
        string calldata purpose,
        bytes calldata signature
    ) external {
        bytes32 scanHash = keccak256(abi.encodePacked(user, amount, purpose));
        require(!scanned[scanHash], "Already scanned");
        require(verify(scanHash, signature), "Invalid sig");

        scanned[scanHash] = true;
        emit VaultScanned(user, amount, purpose, scanHash);
    }

    function verify(bytes32 hash, bytes calldata sig) internal view returns (bool) {
        bytes32 ethHash = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", hash));
        return recoverSigner(ethHash, sig) == signer;
    }

    function recoverSigner(bytes32 _hash, bytes memory _sig) internal pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSig(_sig);
        return ecrecover(_hash, v, r, s);
    }

    function splitSig(bytes memory sig)
        internal
        pure
        returns (bytes32 r, bytes32 s, uint8 v)
    {
        require(sig.length == 65, "bad sig");
        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }
    }
}
