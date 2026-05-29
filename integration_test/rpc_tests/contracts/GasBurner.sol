// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

/**
 * RealGasBurner burns a deterministic, caller-controlled amount of gas by doing
 * real SSTOREs in a loop. Fee-market specs (eth_feeHistory / eth_gasPrice /
 * eth_estimateGas) call `burnGasIterations` to push the base fee up without
 * depending on other suites' traffic.
 *
 * The writes are kept non-trivial (hash-chained into storage) so the optimizer
 * cannot elide them, guaranteeing the gas is actually consumed.
 */
contract RealGasBurner {
    uint256 public accumulator;
    mapping(uint256 => uint256) public sink;

    /**
     * @param salt  Distinguishes otherwise-identical calls so each writes unique slots.
     * @param iterations  Number of storage-writing rounds to perform.
     */
    function burnGasIterations(uint256 salt, uint256 iterations) external {
        uint256 acc = accumulator;
        for (uint256 i = 0; i < iterations; i++) {
            acc = uint256(keccak256(abi.encode(acc, salt, i)));
            sink[acc % 256] = acc;
        }
        accumulator = acc;
    }
}
