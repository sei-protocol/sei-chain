// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * SelfDestructTester
 *
 * Exercises the SELFDESTRUCT opcode against a contract that carries on-chain
 * storage, so there is real account state to clean up. This is a useful
 * determinism edge case for the GIGA store: if the giga executor's account /
 * storage deletion diverges from the V2 executor, a mixed-mode cluster will
 * halt with an AppHash mismatch.
 *
 * Note on EIP-6780 (active under the "prague" EVM version configured for this
 * repo): SELFDESTRUCT only fully deletes the account (code + storage) when it
 * is executed in the SAME transaction that created the contract. When called in
 * a later transaction it merely transfers the contract's balance and leaves the
 * code and storage in place. Both paths are exercised by the tests:
 *   - destroy() on a previously-deployed instance  -> balance sweep only
 *   - SelfDestructFactory.createAndDestroy(...)     -> full account cleanup
 */
contract SelfDestructTester {
    address public owner;
    uint256 public valueA;
    uint256 public valueB;
    // mapping storage so there are multiple distinct storage slots to clean up
    mapping(uint256 => uint256) public data;
    uint256 public numKeys;

    event Destroyed(address indexed recipient, uint256 balance);

    // Constructor seeds storage so a freshly-created contract has state to clean
    // up. Payable so the contract can hold a balance that selfdestruct sweeps.
    constructor(uint256 _numKeys) payable {
        owner = msg.sender;
        valueA = 0xA11CE;
        valueB = 0xB0B;
        numKeys = _numKeys;
        for (uint256 i = 0; i < _numKeys; i++) {
            // non-zero values so the slots are actually written
            data[i] = i + 1;
        }
    }

    function get(uint256 key) external view returns (uint256) {
        return data[key];
    }

    // Sum the seeded keys — handy for asserting storage is intact pre-destruct.
    function sumKeys() external view returns (uint256 total) {
        for (uint256 i = 0; i < numKeys; i++) {
            total += data[i];
        }
    }

    function destroy(address payable recipient) external {
        emit Destroyed(recipient, address(this).balance);
        selfdestruct(recipient);
    }
}

/**
 * SelfDestructFactory
 *
 * Creates a SelfDestructTester (seeding its storage) and destroys it within the
 * same transaction, which under EIP-6780 fully removes the child's code and
 * storage. This is the path that actually forces the store to clean up the
 * account state it just wrote.
 */
contract SelfDestructFactory {
    event ChildCreated(address child);
    event ChildDestroyed(address child);

    function createAndDestroy(uint256 numKeys, address payable recipient)
        external
        payable
        returns (address child)
    {
        SelfDestructTester c = new SelfDestructTester{value: msg.value}(numKeys);
        child = address(c);
        emit ChildCreated(child);
        c.destroy(recipient);
        emit ChildDestroyed(child);
    }
}
