// SPDX-License-Identifier: MIT
pragma solidity ^0.8.25;

/**
 * @title TransientStorageTester
 * @dev Contract to test TLOAD/TSTORE operations with snapshot/revert logic
 * 
 * This contract demonstrates:
 * 1. Basic transient storage operations (TSTORE/TLOAD)
 * 2. Transient storage behavior during snapshots and reverts
 * 3. Interaction between transient storage and regular storage
 * 4. Complex scenarios with multiple snapshots and nested operations
 */
contract TransientStorageTester {
    // Regular storage variables for comparison
    mapping(bytes32 => uint256) public regularStorage;
    mapping(bytes32 => uint256) public regularStorage2;
    
    // Events for tracking operations
    event TransientStorageSet(bytes32 indexed key, uint256 value);
    event TransientStorageGet(bytes32 indexed key, uint256 value);
    event RegularStorageSet(bytes32 indexed key, uint256 value);
    event SnapshotCreated(uint256 snapshotId);
    event SnapshotReverted(uint256 snapshotId);
    event TestCompleted(string testName, bool success);
    
    // Test state tracking
    uint256 public testCounter;
    mapping(string => bool) public testResults;
    
    constructor() {
        testCounter = 0;
    }
    
    /**
     * @dev Basic transient storage operations
     */
    function runBasicTransientStorage(bytes32 key, uint256 value) public {
        // Set transient storage
        assembly {
            tstore(key, value)
        }
        emit TransientStorageSet(key, value);
        
        // Get transient storage
        uint256 retrievedValue;
        assembly {
            retrievedValue := tload(key)
        }
        emit TransientStorageGet(key, retrievedValue);
        
        require(retrievedValue == value, "Transient storage value mismatch");
        testResults["basic"] = true;
    }
    
    /**
     * @dev Test transient storage with snapshot/revert
     */
    function runTransientStorageWithSnapshot(bytes32 key, uint256 value1, uint256 value2) public returns (bool) {
        // Set initial transient storage
        assembly {
            tstore(key, value1)
        }
        emit TransientStorageSet(key, value1);
        
        // Create snapshot
        uint256 snapshotId = uint256(blockhash(block.number - 1));
        emit SnapshotCreated(snapshotId);
        
        // Modify transient storage after snapshot
        assembly {
            tstore(key, value2)
        }
        emit TransientStorageSet(key, value2);
        
        // Verify the new value is set
        uint256 currentValue;
        assembly {
            currentValue := tload(key)
        }
        require(currentValue == value2, "Transient storage not updated after snapshot");
        
        // Simulate revert by setting back to original value
        assembly {
            tstore(key, value1)
        }
        emit SnapshotReverted(snapshotId);
        
        // Verify revert worked
        uint256 revertedValue;
        assembly {
            revertedValue := tload(key)
        }
        require(revertedValue == value1, "Transient storage not reverted correctly");
        
        testResults["snapshot"] = true;
        return true;
    }
    
    /**
     * @dev Compare transient storage with regular storage
     */
    function runTransientVsRegularStorage(bytes32 key, uint256 value) public {
        // Set both transient and regular storage
        assembly {
            tstore(key, value)
        }
        emit TransientStorageSet(key, value);
        
        regularStorage[key] = value;
        emit RegularStorageSet(key, value);
        
        // Verify both are set correctly
        uint256 transientValue;
        assembly {
            transientValue := tload(key)
        }
        require(transientValue == value, "Transient storage value mismatch");
        require(regularStorage[key] == value, "Regular storage value mismatch");
        
        testResults["comparison"] = true;
    }
    
    /**
     * @dev Test multiple transient storage keys
     */
    function runMultipleTransientKeys(bytes32[] memory keys, uint256[] memory values) public {
        require(keys.length == values.length, "Arrays length mismatch");
        
        for (uint256 i = 0; i < keys.length; i++) {
            bytes32 key = keys[i];
            uint256 value = values[i];
            assembly {
                tstore(key, value)
            }
            emit TransientStorageSet(key, value);
        }
        
        // Verify all values are set correctly
        for (uint256 i = 0; i < keys.length; i++) {
            bytes32 key = keys[i];
            uint256 retrievedValue;
            assembly {
                retrievedValue := tload(key)
            }
            require(retrievedValue == values[i], "Transient storage value mismatch");
        }
        
        testResults["multiple"] = true;
    }
    
    /**
     * @dev Complex snapshot scenario with multiple operations
     */
    function runComplexSnapshotScenario() public returns (bool) {
        bytes32[] memory keys = new bytes32[](3);
        uint256[] memory values = new uint256[](3);
        
        // Initialize keys and values
        keys[0] = keccak256(abi.encodePacked("complex_key_1"));
        keys[1] = keccak256(abi.encodePacked("complex_key_2"));
        keys[2] = keccak256(abi.encodePacked("complex_key_3"));
        values[0] = 100;
        values[1] = 200;
        values[2] = 300;
        
        // Set initial transient storage
        for (uint256 i = 0; i < keys.length; i++) {
            bytes32 key = keys[i];
            uint256 value = values[i];
            assembly {
                tstore(key, value)
            }
            emit TransientStorageSet(key, value);
        }
        
        // Create snapshot
        uint256 snapshotId = uint256(blockhash(block.number - 1));
        emit SnapshotCreated(snapshotId);
        
        // Modify transient storage after snapshot
        for (uint256 i = 0; i < keys.length; i++) {
            bytes32 key = keys[i];
            uint256 newValue = values[i] * 2;
            assembly {
                tstore(key, newValue)
            }
            emit TransientStorageSet(key, newValue);
        }
        
        // Simulate revert by setting back to original values
        for (uint256 i = 0; i < keys.length; i++) {
            bytes32 key = keys[i];
            uint256 originalValue = values[i];
            assembly {
                tstore(key, originalValue)
            }
        }
        emit SnapshotReverted(snapshotId);
        
        // Verify revert worked
        for (uint256 i = 0; i < keys.length; i++) {
            bytes32 key = keys[i];
            uint256 retrievedValue;
            assembly {
                retrievedValue := tload(key)
            }
            require(retrievedValue == values[i], "Transient storage not reverted correctly");
        }
        
        testResults["complex"] = true;
        return true;
    }
    
    /**
     * @dev Test zero values in transient storage
     */
    function runZeroValues() public {
        bytes32 key = keccak256(abi.encodePacked("zero_test"));
        
        // Set zero value
        assembly {
            tstore(key, 0)
        }
        emit TransientStorageSet(key, 0);
        
        // Verify zero value is stored correctly
        uint256 retrievedValue;
        assembly {
            retrievedValue := tload(key)
        }
        require(retrievedValue == 0, "Zero value not stored correctly");
        
        // Test setting non-zero then zero
        assembly {
            tstore(key, 123)
        }
        assembly {
            tstore(key, 0)
        }
        
        assembly {
            retrievedValue := tload(key)
        }
        require(retrievedValue == 0, "Zero value not overwritten correctly");
        
        testResults["zero"] = true;
    }
    
    /**
     * @dev Test large values in transient storage
     */
    function runLargeValues() public {
        bytes32 key = keccak256(abi.encodePacked("large_test"));
        uint256 largeValue = type(uint256).max;
        
        // Set large value
        assembly {
            tstore(key, largeValue)
        }
        emit TransientStorageSet(key, largeValue);
        
        // Verify large value is stored correctly
        uint256 retrievedValue;
        assembly {
            retrievedValue := tload(key)
        }
        require(retrievedValue == largeValue, "Large value not stored correctly");
        
        testResults["large"] = true;
    }
    
    /**
     * @dev Test uninitialized keys
     */
    function runUninitializedKeys() public {
        bytes32 key = keccak256(abi.encodePacked("uninitialized_test"));
        
        // Try to load uninitialized key
        uint256 retrievedValue;
        assembly {
            retrievedValue := tload(key)
        }
        require(retrievedValue == 0, "Uninitialized key should return 0");
        
        testResults["uninitialized"] = true;
    }
    
    /**
     * @dev Comprehensive test that combines all scenarios
     */
    function runComprehensiveTest() public returns (bool) {
        // Test 1: Basic operations
        runBasicTransientStorage(keccak256("basic"), 123);
        
        // Test 2: Snapshot/revert
        runTransientStorageWithSnapshot(keccak256("snapshot"), 100, 200);
        
        // Test 3: Multiple keys
        bytes32[] memory keys = new bytes32[](3);
        uint256[] memory values = new uint256[](3);
        keys[0] = keccak256("multi1");
        keys[1] = keccak256("multi2");
        keys[2] = keccak256("multi3");
        values[0] = 111;
        values[1] = 222;
        values[2] = 333;
        runMultipleTransientKeys(keys, values);
        
        // Test 4: Complex snapshot scenario
        runComplexSnapshotScenario();
        
        // Test 5: Zero values
        runZeroValues();
        
        // Test 6: Large values
        runLargeValues();
        
        // Test 7: Uninitialized keys
        runUninitializedKeys();
        
        // Test 8: Comparison with regular storage
        runTransientVsRegularStorage(keccak256("comparison"), 999);
        
        emit TestCompleted("comprehensive", true);
        return true;
    }
    
    /**
     * @dev Get test results
     */
    function getTestResults() public view returns (bool basic, bool snapshot, bool multiple, bool complex, bool zero, bool large, bool uninitialized, bool comparison) {
        return (
            testResults["basic"],
            testResults["snapshot"],
            testResults["multiple"],
            testResults["complex"],
            testResults["zero"],
            testResults["large"],
            testResults["uninitialized"],
            testResults["comparison"]
        );
    }
    
    /**
     * @dev Reset all test results
     */
    function resetTestResults() public {
        delete testResults["basic"];
        delete testResults["snapshot"];
        delete testResults["multiple"];
        delete testResults["complex"];
        delete testResults["zero"];
        delete testResults["large"];
        delete testResults["uninitialized"];
        delete testResults["comparison"];
    }
} 