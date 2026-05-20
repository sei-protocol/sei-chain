// SPDX-License-Identifier: MIT
// Contract used by the ws_test eth_subscribe(logs) integration test. The
// constructor emits one LOG1 with a fixed 32-byte topic; the deploy tx
// receipt therefore contains exactly one log, whose address equals the
// newly-created contract address. The test subscribes to the deploy
// block + emitter address and asserts the log is delivered over the WS.
//
// emitter_contract.hex is hand-crafted (no solc required). Init code:
//
//   7f<32-byte topic>  PUSH32 topic   (0x4242…4242, easy to spot in logs)
//   6000               PUSH1 0        ; LOG data size
//   6000               PUSH1 0        ; LOG data offset
//   a1                 LOG1
//   6001               PUSH1 1        ; runtime size
//   6000               PUSH1 0        ; runtime offset (memory is zero-init,
//                                       so memory[0] = 0x00 = STOP)
//   f3                 RETURN
//
// Solidity reference (not compiled — only the .hex is used at deploy time):

pragma solidity ^0.8.0;

contract Emitter {
    constructor() {
        assembly {
            log1(0, 0, 0x4242424242424242424242424242424242424242424242424242424242424242)
        }
    }
}
