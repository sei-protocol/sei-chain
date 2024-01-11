// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;

import {Test, console2} from "forge-std/Test.sol";
import {CW721ERC721Wrapper} from "../src/CW721ERC721Wrapper.sol";
import {IWasmd} from "../src/precompiles/IWasmd.sol";
import {IJson} from "../src/precompiles/IJson.sol";
import {IAddr} from "../src/precompiles/IAddr.sol";

address constant WASMD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001002;
address constant JSON_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001003;
address constant ADDR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001004;

address constant MockCallerEVMAddr = 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266;
address constant MockOperatorEVMAddr = 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267;
string constant MockCallerSeiAddr = "sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw";
string constant MockOperatorSeiAddr = "sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe";
string constant MockCWContractAddress = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m";

contract MockWasmd is IWasmd {

    // Transactions
    function instantiate(
        uint64,
        string memory,
        bytes memory,
        string memory,
        bytes memory
    ) external pure returns (string memory, bytes memory) {
        return (MockCWContractAddress, bytes(""));
    }

    function execute(
        string memory contractAddress,
        bytes memory,
        bytes memory
    ) external pure returns (bytes memory) {
        require(keccak256(abi.encodePacked(contractAddress)) == keccak256(abi.encodePacked(MockCWContractAddress)), "wrong CW contract address");
        return bytes("");
    }

    // Queries
    function query(string memory, bytes memory) external pure returns (bytes memory) {
        return bytes("");
    }
}

contract MockJson is IJson {
    function extractAsBytes(bytes memory, string memory) external pure returns (bytes memory) {
        return bytes("extracted bytes");
    }

    function extractAsBytesList(bytes memory, string memory) external pure returns (bytes[] memory) {
        return new bytes[](0);
    }

    function extractAsUint256(bytes memory input, string memory key) external view returns (uint256 response) {
        return 0;
    }
}

contract MockAddr is IAddr {
    function getSeiAddr(address addr) external pure returns (string memory) {
        if (addr == MockCallerEVMAddr) {
            return MockCallerSeiAddr;
        }
        return MockOperatorSeiAddr;
    }

    function getEvmAddr(string memory addr) external pure returns (address) {
        if (keccak256(abi.encodePacked(addr)) == keccak256(abi.encodePacked(MockCallerSeiAddr))) {
            return MockCallerEVMAddr;
        }
        return MockOperatorEVMAddr;
    }
}

contract CW721ERC721WrapperTest is Test {
    event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);
    event Approval(address indexed owner, address indexed approved, uint256 indexed tokenId);
    event ApprovalForAll(address indexed owner, address indexed operator, bool approved);

    CW721ERC721Wrapper wrapper;
    MockWasmd mockWasmd;
    MockJson mockJson;
    MockAddr mockAddr;

    function setUp() public {
        wrapper = new CW721ERC721Wrapper(MockCWContractAddress, "name", "symbol");
        mockWasmd = new MockWasmd();
        mockJson = new MockJson();
        mockAddr = new MockAddr();
        vm.etch(WASMD_PRECOMPILE_ADDRESS, address(mockWasmd).code);
        vm.etch(JSON_PRECOMPILE_ADDRESS, address(mockJson).code);
        vm.etch(ADDR_PRECOMPILE_ADDRESS, address(mockAddr).code);
    }

    function testName() public {
        assertEq(wrapper.name(), "name");
    }

    function testSymbol() public {
        assertEq(wrapper.symbol(), "symbol");
    }

    function testBalanceOf() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, bytes("{\"tokens\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}}")),
            abi.encode("{\"tokens\":[\"a\",\"b\"]}")
        );
        bytes[] memory response = new bytes[](2);
        response[0] = bytes("a");
        response[1] = bytes("b");
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytesList(bytes,string)", bytes("{\"tokens\":[\"a\",\"b\"]}"), "tokens"),
            abi.encode(response)
        );
        assertEq(wrapper.balanceOf(MockCallerEVMAddr), 2);
    }

    function testOwnerOf() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, bytes("{\"owner_of\":{\"token_id\":\"1\"}}")),
            abi.encode("{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}")
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}"), "owner"),
            abi.encode(bytes("sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw"))
        );
        assertEq(wrapper.ownerOf(1), 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
    }

    function testGetApproved() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, bytes("{\"approval\":{\"spender\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\",\"token_id\":\"1\"}}")),
            abi.encode("{\"approval\":{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}")
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"approval\":{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}"), "approval"),
            abi.encode(bytes("{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}"))
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}"), "spender"),
            abi.encode(bytes("sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe"))
        );
        vm.startPrank(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
        assertEq(wrapper.getApproved(1), 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267);
        vm.stopPrank();
    }

    function testIsApprovedForAll() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, bytes("{\"all_operators\":{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}}")),
            abi.encode("{\"operators\":[{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}]}")
        );
        bytes[] memory response = new bytes[](1);
        response[0] = bytes("{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}");
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytesList(bytes,string)", bytes("{\"operators\":[{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}]}"), "operators"),
            abi.encode(response)
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}"), "spender"),
            abi.encode(bytes("sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe"))
        );
        assertEq(wrapper.isApprovedForAll(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266, 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267), true);
    }

    function testTransferFrom() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, bytes("{\"transfer_nft\":{\"recipient\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\",\"token_id\":\"1\"}}"), bytes("[]")),
            abi.encode(bytes(""))
        );
        vm.startPrank(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
        vm.expectEmit();
        emit Transfer(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266, 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, 1);
        wrapper.transferFrom(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266, 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, 1);
        vm.stopPrank();
    }

    function testApprove() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, bytes("{\"approve\":{\"spender\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\",\"token_id\":\"1\"}}"), bytes("[]")),
            abi.encode(bytes(""))
        );
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("query(string,bytes)", MockCWContractAddress, bytes("{\"owner_of\":{\"token_id\":\"1\"}}")),
            abi.encode("{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}")
        );
        vm.mockCall(
            JSON_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("extractAsBytes(bytes,string)", bytes("{\"owner\":\"sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw\"}"), "owner"),
            abi.encode(bytes("sei19zhelek4q5lt4zam8mcarmgv92vzgqd3ux32jw"))
        );
        vm.startPrank(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
        vm.expectEmit();
        emit Approval(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266, 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, 1);
        wrapper.approve(0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, 1);
        vm.stopPrank();
    }

    function testSetApprovalForAll() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, bytes("{\"approval_all\":{\"operator\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}"), bytes("[]")),
            abi.encode(bytes(""))
        );
        vm.startPrank(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
        vm.expectEmit();
        emit ApprovalForAll(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266, 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, true);
        wrapper.setApprovalForAll(0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, true);
        vm.stopPrank();
    }

    function testSetRevokeForAll() public {
        vm.mockCall(
            WASMD_PRECOMPILE_ADDRESS,
            abi.encodeWithSignature("execute(string,bytes,bytes)", MockCWContractAddress, bytes("{\"revoke_all\":{\"operator\":\"sei1vldxw5dy5k68hqr4d744rpg9w8cqs54x4asdqe\"}}"), bytes("[]")),
            abi.encode(bytes(""))
        );
        vm.startPrank(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266);
        vm.expectEmit();
        emit ApprovalForAll(0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266, 0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, false);
        wrapper.setApprovalForAll(0xF39fD6e51Aad88F6f4CE6AB8827279CFffb92267, false);
        vm.stopPrank();
    }
}