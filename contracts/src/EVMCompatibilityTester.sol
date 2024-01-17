// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC721/IERC721.sol";

contract EVMCompatibilityTester {
    // verify different events with var types
    event ActionPerformed(string action, address indexed performer);
    event BoolSet(address performer, bool value);
    event AddressSet(address indexed performer);
    event Uint256Set(address indexed performer, uint256 value);
    event StringSet(address indexed performer, string value);

    struct MsgDetails {
        address sender;
        uint256 value;
        bytes data;
        uint256 gas;
    }

    // Example of contract storing and retrieving data
    uint256 private storedData;

    // one of each type
    address public addressVar;
    bool public boolVar;
    uint256 public uint256Var;
    string public stringVar;

    // State variable to store the details
    MsgDetails public lastMsgDetails;

    mapping(address => uint256) public balances;

    function storeData(uint256 data) public {
        storedData = data;
        emit ActionPerformed("Data Stored", msg.sender);
    }

    // Function to set a balance for a specific address
    function setBalance(address user, uint256 amount) public {
        balances[user] = amount;
        emit ActionPerformed("Balance Set", msg.sender);
    }

    function setAddressVar() public {
        addressVar = msg.sender;
        emit AddressSet(msg.sender);
    }

    function setBoolVar(bool value) public {
        boolVar = value;
        emit BoolSet(msg.sender, value);
    }

    function setStringVar(string memory value) public {
        stringVar = value;
        emit StringSet(msg.sender, value);
    }

    function setUint256Var(uint256 value) public {
        uint256Var = value;
        emit Uint256Set(msg.sender, value);
    }

    // verify returning of private var
    function retrieveData() public view returns (uint256) {
        return storedData;
    }

    // Example of inter-contract calls
    function callAnotherContract(address contractAddress, bytes memory data) public {
        (bool success, ) = contractAddress.call(data);
        require(success, "Call failed");
        emit ActionPerformed("Inter-Contract Call", msg.sender);
    }

    // Example of inline assembly: a simple function to add two numbers
    function addNumbers(uint256 a, uint256 b) public pure returns (uint256 sum) {
        assembly {
            sum := add(a, b)
        }
    }

    // Inline assembly for accessing contract balance
    function getContractBalance() public view returns (uint256 contractBalance) {
        assembly {
            contractBalance := selfbalance()
        }
    }

    function getBlockProperties() public view returns (bytes32 blockHash, address payable coinbase, uint prevrandao, uint gaslimit, uint number, uint timestamp) {
        blockHash = blockhash(block.number - 1);
        coinbase = block.coinbase;
        prevrandao = block.prevrandao;
        gaslimit = block.gaslimit;
        number = block.number;
        timestamp = block.timestamp;

        return (blockHash, coinbase, prevrandao, gaslimit, number, timestamp);
    }


    function revertIfFalse(bool value) public {
        boolVar = value;
        require(value == true, "value must be true");
    }

    // More complex example: Inline assembly to read from storage directly
    function readFromStorage(uint256 storageIndex) public view returns (uint256 data) {
        assembly {
            data := sload(storageIndex)
        }
    }

    // Function to store some properties of 'msg'
    function storeMsgProperties() public payable {
        // Storing the properties of 'msg'
        lastMsgDetails = MsgDetails({
            sender: msg.sender,
            value: msg.value,
            data: msg.data,
            gas: gasleft()
        });
    }
}

