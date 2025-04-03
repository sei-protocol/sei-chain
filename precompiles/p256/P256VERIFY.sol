// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant P256VERIFY_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001011;

IP256VERIFY constant P256VERIFY_CONTRACT = IP256VERIFY(P256VERIFY_PRECOMPILE_ADDRESS);

interface IP256VERIFY {
    function verify(
        bytes memory signature
    ) external view returns (bytes memory response);
}