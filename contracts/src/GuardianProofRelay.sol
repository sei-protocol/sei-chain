// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IZKVerifier {
    function verify(bytes calldata proof) external view returns (bool);
}

contract GuardianProofRelay {
    address public owner;
    address public verifier;
    address public guardian;

    mapping(bytes32 => bool) public processedProofs;

    event RelaySuccess(address indexed sender, bytes32 proofHash, string method);
    event GuardianUpdated(address indexed newGuardian);
    event VerifierUpdated(address indexed newVerifier);

    error Unauthorized();
    error InvalidSignature();
    error InvalidGuardian();
    error InvalidVerifier();
    error ProofAlreadyProcessed(bytes32 proofHash);
    error VerificationFailed();

    constructor(address _verifier, address _guardian) {
        if (_guardian == address(0)) {
            revert InvalidGuardian();
        }
        if (_verifier == address(0)) {
            revert InvalidVerifier();
        }
        owner = msg.sender;
        verifier = _verifier;
        guardian = _guardian;
    }

    modifier onlyOwner() {
        if (msg.sender != owner) {
            revert Unauthorized();
        }
        _;
    }

    function relaySignedProof(bytes calldata proof, bytes calldata signature) external {
        bytes32 proofHash = keccak256(proof);
        if (processedProofs[proofHash]) {
            revert ProofAlreadyProcessed(proofHash);
        }

        bytes32 digest = _hashProof(proofHash);
        if (!_verifySig(digest, signature)) {
            revert InvalidSignature();
        }

        processedProofs[proofHash] = true;
        emit RelaySuccess(msg.sender, proofHash, "SignedProof");
    }

    function relayZKProof(bytes calldata proof) external {
        bytes32 proofHash = keccak256(proof);
        if (processedProofs[proofHash]) {
            revert ProofAlreadyProcessed(proofHash);
        }

        if (!IZKVerifier(verifier).verify(proof)) {
            revert VerificationFailed();
        }

        processedProofs[proofHash] = true;
        emit RelaySuccess(msg.sender, proofHash, "ZKProof");
    }

    function updateGuardian(address newGuardian) external onlyOwner {
        if (newGuardian == address(0)) {
            revert InvalidGuardian();
        }
        guardian = newGuardian;
        emit GuardianUpdated(newGuardian);
    }

    function updateVerifier(address newVerifier) external onlyOwner {
        if (newVerifier == address(0)) {
            revert InvalidVerifier();
        }
        verifier = newVerifier;
        emit VerifierUpdated(newVerifier);
    }

    function _hashProof(bytes32 proofHash) private view returns (bytes32) {
        return keccak256(abi.encodePacked(address(this), block.chainid, proofHash));
    }

    function _verifySig(bytes32 digest, bytes calldata signature) private view returns (bool) {
        if (signature.length != 65) {
            return false;
        }

        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(signature.offset)
            s := calldataload(add(signature.offset, 0x20))
            v := byte(0, calldataload(add(signature.offset, 0x40)))
        }

        if (v < 27) {
            v += 27;
        }

        if (v != 27 && v != 28) {
            return false;
        }

        bytes32 ethSignedHash = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", digest));
        address signer = ecrecover(ethSignedHash, v, r, s);
        return signer == guardian;
    }
}
