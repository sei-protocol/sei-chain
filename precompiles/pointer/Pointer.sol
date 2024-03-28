// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant POINTER_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001009;

IPointer constant POINTER_CONTRACT = IPointer(POINTER_PRECOMPILE_ADDRESS);

interface IPointer {
    function addNativePointer(
        string memory token
    ) payable external returns (address ret);

    function addCW20Pointer(
        string memory cwAddr
    ) payable external returns (address ret);

    function addCW721Pointer(
        string memory cwAddr
    ) payable external returns (address ret);
}
