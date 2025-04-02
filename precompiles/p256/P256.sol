// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant P256_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001011;

IP256 constant P256_CONTRACT = IP256(P256_PRECOMPILE_ADDRESS);

interface IP256 {
    function verify(
        bytes memory signature
    ) external view returns (bytes memory response);
}