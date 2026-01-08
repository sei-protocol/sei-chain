// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/**
 * @title SstoreGasTest
 * @notice Contract to test SSTORE gas accounting differences between receipts and debug traces
 * @dev This contract performs cold SSTORE operations to reproduce the gas discrepancy issue
 */
contract SstoreGasTest {
    // Storage slots that will be written to (cold SSTORE)
    uint256 public slot0;
    uint256 public slot1;
    uint256 public slot2;
    uint256 public slot3;
    uint256 public slot4;
    
    // Mapping for additional cold storage writes
    mapping(uint256 => uint256) public data;
    
    event StorageWritten(uint256 indexed count, uint256 gasUsed);
    
    /**
     * @notice Performs a single cold SSTORE operation
     * @param value The value to store
     */
    function singleColdSstore(uint256 value) external {
        uint256 gasBefore = gasleft();
        slot0 = value;
        uint256 gasAfter = gasleft();
        emit StorageWritten(1, gasBefore - gasAfter);
    }
    
    /**
     * @notice Performs multiple cold SSTORE operations
     * @param value The base value to store
     */
    function multipleColdSstores(uint256 value) external {
        uint256 gasBefore = gasleft();
        slot0 = value;
        slot1 = value + 1;
        slot2 = value + 2;
        slot3 = value + 3;
        slot4 = value + 4;
        uint256 gasAfter = gasleft();
        emit StorageWritten(5, gasBefore - gasAfter);
    }
    
    /**
     * @notice Performs cold SSTORE to mapping slots
     * @param count Number of mapping entries to write
     * @param baseValue Base value to store
     */
    function coldSstoreMapping(uint256 count, uint256 baseValue) external {
        uint256 gasBefore = gasleft();
        for (uint256 i = 0; i < count; i++) {
            data[i] = baseValue + i;
        }
        uint256 gasAfter = gasleft();
        emit StorageWritten(count, gasBefore - gasAfter);
    }
    
    /**
     * @notice Performs warm SSTORE (re-writing to same slot)
     * @param value The value to store
     */
    function warmSstore(uint256 value) external {
        // First write (cold)
        slot0 = value;
        // Second write to same slot (warm)
        slot0 = value + 1;
    }
    
    /**
     * @notice Mixed cold and warm SSTOREs
     * @param value The value to store
     */
    function mixedSstores(uint256 value) external {
        // Cold writes
        slot0 = value;
        slot1 = value + 1;
        // Warm writes (same slots)
        slot0 = value + 10;
        slot1 = value + 11;
        // More cold writes
        slot2 = value + 2;
    }
    
    /**
     * @notice Reset all slots to zero (for testing clean state)
     */
    function resetSlots() external {
        slot0 = 0;
        slot1 = 0;
        slot2 = 0;
        slot3 = 0;
        slot4 = 0;
    }
    
    /**
     * @notice Get current slot values
     */
    function getSlots() external view returns (uint256, uint256, uint256, uint256, uint256) {
        return (slot0, slot1, slot2, slot3, slot4);
    }
}

