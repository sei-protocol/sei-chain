// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {ECDSA} from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";

import {IBeaconVerifier} from "./interfaces/IBeaconVerifier.sol";

contract BeaconVerifier is IBeaconVerifier, Ownable {
    using ECDSA for bytes32;

    error InvalidSigner(address signer);

    event ValidatorSignerUpdated(address indexed previousSigner, address indexed newSigner);

    address public validatorSigner;

    constructor(address initialSigner) Ownable(msg.sender) {
        if (initialSigner == address(0)) {
            revert InvalidSigner(address(0));
        }

        validatorSigner = initialSigner;
    }

    function verifyBeaconSignature(
        address user,
        bytes32 wifiHash,
        bytes calldata sig
    ) external view override returns (bool) {
        if (user == address(0) || wifiHash == bytes32(0) || sig.length == 0) {
            return false;
        }

        bytes32 digest = keccak256(abi.encodePacked(user, wifiHash)).toEthSignedMessageHash();
        (address recovered, ECDSA.RecoverError errorCode) = ECDSA.tryRecover(digest, sig);
        if (errorCode != ECDSA.RecoverError.NoError) {
            return false;
        }

        return recovered == validatorSigner;
    }

    function updateSigner(address newSigner) external onlyOwner {
        if (newSigner == address(0)) {
            revert InvalidSigner(address(0));
        }

        address previousSigner = validatorSigner;
        validatorSigner = newSigner;

        emit ValidatorSignerUpdated(previousSigner, newSigner);
    }
}
