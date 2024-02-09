// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC721/IERC721.sol";
import "./TestToken.sol";

contract EVMCompatibilityTester {
    // verify different events with var types
    event DummyEvent(string indexed str, bool flag, address indexed addr, uint256 indexed num, bytes data);
    event ActionPerformed(string action, address indexed performer);
    event BoolSet(address performer, bool value);
    event AddressSet(address indexed performer);
    event Uint256Set(address indexed performer, uint256 value);
    event StringSet(address indexed performer, string value);
    event LogIndexEvent(address indexed performer, uint256 value);

    struct MsgDetails {
        address sender;
        uint256 value;
        bytes data;
        uint256 gas;
    }

    // Example of contract storing and retrieving data
    uint256 private storedData;

    // deployer of the contract
    address public owner;

    // one of each type
    address public addressVar;
    bool public boolVar;
    uint256 public uint256Var;
    string public stringVar;

    // State variable to store the details
    MsgDetails public lastMsgDetails;

    mapping(address => uint256) public balances;

    constructor() {
        owner = msg.sender;
    }

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

    function emitMultipleLogs(uint256 count) public {
        for (uint256 i = 0; i < count; i++) {
            emit LogIndexEvent(msg.sender, i);
        }
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

    // Example of creating a new contract from a contract
    function createToken(string memory name, string memory symbol) public {
        TestToken token = new TestToken(name, symbol);
        token.transfer(msg.sender, 100);
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

    function depositEther() external payable {
        require(msg.value > 0, "No Ether sent");
    }

    function sendEther(address payable recipient, uint256 amount) external payable {
        require(msg.sender == owner, "Only owner can send Ether");
        require(address(this).balance >= amount, "Insufficient balance");
        recipient.transfer(amount);
    }

    function emitDummyEvent(string memory str, uint256 num) external {
        bytes memory bytes_ = bytes(string(abi.encodePacked(str, "Bytes")));
        emit DummyEvent(str, true, msg.sender, num, bytes_);
    }
}

