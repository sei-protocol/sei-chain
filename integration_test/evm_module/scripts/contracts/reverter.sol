// SPDX-License-Identifier: MIT
// Contract used by eth_call revert .iox tests. Calldata: 0x01 or empty -> Error("user error"); 0x02 -> panic (assert false).
// To rebuild bytecode: from this directory: solc --bin reverter.sol | tail -1 > reverter_contract.hex (committed hex built with solc 0.8.28)

pragma solidity ^0.8.0;

contract Reverter {
    fallback() external payable {
        if (msg.data.length >= 1 && msg.data[0] == 0x02) {
            assert(false); // Panic(0x01)
        }
        revert("user error");
    }
}
