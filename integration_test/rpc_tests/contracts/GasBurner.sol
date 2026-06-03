// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

/**
 * RealGasBurner burns a deterministic, caller-controlled amount of gas by doing
 * real SSTOREs in a loop. Fee-market specs (eth_feeHistory / eth_gasPrice /
 * eth_estimateGas) call `burnGasIterations` to push the base fee up without
 * depending on other suites' traffic.
 *
 * Crucially, every iteration writes a brand-new storage slot (keyed by a global,
 * monotonically increasing counter), so each SSTORE is a cold zero->nonzero write
 * (~22.1k gas). That keeps the per-iteration cost stable and predictable across
 * calls — callers size their burns as `iterations ≈ targetGas / 22_300` and rely
 * on a single tx actually consuming that much, which only holds if slots are never
 * reused (reusing slots makes later writes ~5k/warm and under-burns the block).
 */
contract RealGasBurner {
    uint256 public accumulator;
    uint256 public writeCount;
    mapping(uint256 => uint256) public sink;

    constructor() {
        // Pre-warm the bookkeeping slots to non-zero. burnGasIterations only ever does
        // nonzero->nonzero writes to them afterwards, so its per-call gas cost is constant
        // from the very first invocation. Otherwise the first call would pay a one-time
        // zero->nonzero bump (~+17k each) and an early gas estimate would diverge from a
        // later one taken after the slots were initialised.
        accumulator = 1;
        writeCount = 1;
    }

    /**
     * @param salt  Mixed into the hash chain so identical iteration counts still do
     *              distinct work; does not affect the (unique) slot being written.
     * @param iterations  Number of fresh-slot SSTORE rounds to perform.
     */
    function burnGasIterations(uint256 salt, uint256 iterations) external {
        uint256 acc = accumulator;
        uint256 n = writeCount;
        for (uint256 i = 0; i < iterations; i++) {
            acc = uint256(keccak256(abi.encode(acc, salt, i)));
            // `n` is globally unique for the contract's lifetime, so this slot has
            // never been written -> guaranteed cold zero->nonzero SSTORE (~22.1k gas).
            // `| 1` guarantees the stored value is non-zero.
            sink[n] = acc | 1;
            n++;
        }
        writeCount = n;
        accumulator = acc;
    }
}
